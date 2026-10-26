# GNU make's built-in `%.out: %` rule deletes an expected-output fixture and copies its input
# directory over it whenever the directory is newer, so no built-in rule may apply here.
MAKEFLAGS += --no-builtin-rules

GO_DIRS := $(shell go list -f '{{.Dir}}' ./...)
GO_FILES := $(patsubst $(CURDIR)/%,%,$(wildcard $(addsuffix /*.go,$(GO_DIRS))))
SRCS := $(filter-out %_test.go, $(GO_FILES)) internal/config/config_default.yaml internal/actioncatalog/popular_actions.json internal/webhook/payload/catalogue.json go.mod go.sum
TESTS := $(filter %_test.go, $(GO_FILES))
TOOL := $(wildcard tools/config/*.go internal/*/generate/*.go internal/*/metadata/*.go tools/release/bump-version/*.go internal/*/check-*/*.go)
TESTDATA := $(wildcard \
		testdata/examples/* \
		testdata/err/* \
		testdata/ok/* \
		testdata/config/* \
		testdata/format/* \
		testdata/projects/* \
		testdata/reusable_workflow_metadata/* \
	)
GO_GEN_SRCS := generate.go $(filter-out %_test.go,$(wildcard internal/actioncatalog/generate/*.go internal/actioncatalog/metadata/*.go)) \
				internal/actioncatalog/generate/popular_actions.json \
				$(filter-out %_test.go,$(wildcard internal/webhook/generate/*.go)) \
				$(wildcard internal/webhook/generate/sources/*) \
				$(filter-out %_test.go,$(wildcard internal/webhook/payload/*.go)) \
				internal/expression/context/generate/main.go
PAYLOAD_GEN_SRCS := generate.go $(filter-out %.test.mts,$(wildcard internal/webhook/payload/generate/*.mts)) \
				internal/webhook/payload/generate/source.json
PANDOC := pandoc --standalone --from=markdown-smart --syntax-highlighting=none

ifeq ($(OS),Windows_NT)
	SHELL := powershell.exe
	.SHELLFLAGS := -NoProfile -ExecutionPolicy Bypass -Command
	TARGET = actionlint.exe
	TOUCH = powershell -NoProfile -ExecutionPolicy Bypass tools/ci/touch.ps1
	# It's hard to prepare C toolchain for CGO on Windows
	RACE =
else
	TARGET = actionlint
	TOUCH = touch
	RACE = -race
endif

# The race detector makes the suite roughly twenty times slower, which is a poor
# trade in an agent session that runs it after every edit. Setting NORACE=1 turns
# it off, and CLAUDECODE=1 does the same, since Claude Code sets that itself.
ifeq ($(CLAUDECODE),1)
	RACE =
endif
ifeq ($(NORACE),1)
	RACE =
endif

# Probe the race runtime before the suite: some ARM64 kernels have an
# unsupported virtual address layout. Keep running ordinary tests on those hosts.
ifeq ($(OS),Windows_NT)
GO_TEST = go test $(RACE) $(1)
else
define GO_TEST
@race='$(RACE)'; \
if [ -n "$$race" ]; then \
	if probe="$$(go test $$race -run '^$$' -count=1 ./internal/projectpath 2>&1)"; then \
		:; \
	elif printf '%s\n' "$$probe" | grep -Fq 'ThreadSanitizer: unsupported VMA range'; then \
		printf '%s\n' 'Race detector unavailable on this kernel; running tests without -race.'; \
		race=; \
	else \
		printf '%s\n' "$$probe" >&2; \
		exit 1; \
	fi; \
fi; \
echo "go test$${race:+ $$race} $(1)"; \
go test $$race $(1)
endef
endif

all: build test lint

comment-cop:
	node .github/actions/comment-cop/comment-cop.mjs $(COMMENT_COP_BASE)

test:
	$(call GO_TEST,./...)

.PHONY: conformance
conformance:
	go run ./internal/conformance/fetch
	go test -tags conformance -run '^TestUpstreamConformance' -count=1 -timeout 2m ./internal/conformance

coverage.out: $(TESTS) $(SRCS) $(TESTDATA) $(TOOL)
	$(call GO_TEST,-coverprofile coverage.out -covermode=atomic ./...)

coverage.html: coverage.out
	go tool cover -html=coverage.out -o coverage.html

cov: coverage.out coverage.html
	go tool cover -func=coverage.out

lint:
	golangci-lint run
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...
ifneq ($(OS),Windows_NT)
	GOOS=js GOARCH=wasm golangci-lint run ./playground
endif

# Documentation examples use the configured external script checkers.
.PHONY: lint-docs
lint-docs:
	go run ./internal/rulecatalog/check-docs -quiet ./docs/checks.md

internal/webhook/payload/catalogue.json: $(PAYLOAD_GEN_SRCS)
ifdef SKIP_GO_GENERATE
	$(TOUCH) $@
else
	go generate -run 'payload/generate'
endif

internal/actioncatalog/popular_actions.json internal/webhook/webhooks.gen.go internal/expression/context/availability.gen.go internal/action/availability.gen.go internal/action/runtimes.gen.go &: $(GO_GEN_SRCS) internal/webhook/payload/catalogue.json
ifdef SKIP_GO_GENERATE
	$(TOUCH) internal/actioncatalog/popular_actions.json internal/webhook/webhooks.gen.go internal/expression/context/availability.gen.go internal/action/availability.gen.go internal/action/runtimes.gen.go
else
	go generate -skip 'payload/generate'
endif

GIT_DESCRIBE := $(shell git describe --tags 2>/dev/null)
ifneq ($(GIT_DESCRIBE),)
BUILD_LDFLAGS = -ldflags "-X actionlint.kjanat.dev/v2/internal/buildinfo.ReleaseVersion=$(GIT_DESCRIBE)"
endif

# Creating or moving a tag changes the version stamp without touching a prerequisite.
$(TARGET): $(SRCS) FORCE
ifeq ($(OS),Windows_NT)
	go build $(BUILD_LDFLAGS) ./cmd/actionlint
else
	CGO_ENABLED=0 go build $(BUILD_LDFLAGS) ./cmd/actionlint
endif

FORCE:

build: $(TARGET)

# go test -fuzz accepts exactly one target.
fuzz:
ifdef FUZZ_FUNC
	go test -run '^$$' -fuzz '^$(FUZZ_FUNC)$$' ./fuzz
else
	go test -list '^Fuzz' ./fuzz
endif

man/actionlint.1: man/actionlint.1.md man/inline-code-bold.lua
	$(PANDOC) --to=man --metadata=title:ACTIONLINT --lua-filter=man/inline-code-bold.lua --output=$@ $<

man/actionlint.1.html: man/actionlint.1.md man/manual.css
	$(PANDOC) --to=html --css=manual.css --output=$@ $<

man: man/actionlint.1 man/actionlint.1.html

bench:
	go test -run '^$$' -bench Lint -benchmem ./internal/application

.github/actionlint-matcher.json: tools/matcher/main.mjs tools/matcher/object.mjs
	node ./tools/matcher/main.mjs .github/actionlint-matcher.json

tools/matcher/testdata/escape.txt: $(TARGET)
	./actionlint -color ./testdata/err/one_error.yaml > ./tools/matcher/testdata/escape.txt || true
tools/matcher/testdata/no_escape.txt: $(TARGET)
	./actionlint -no-color ./testdata/err/one_error.yaml > ./tools/matcher/testdata/no_escape.txt || true
tools/matcher/testdata/want.json: $(TARGET)
	./actionlint -format '{{json .}}' ./testdata/err/one_error.yaml > tools/matcher/testdata/want.json || true

PLUGIN_FIXTURES := internal/plugins/component/testdata/plugins
PLUGIN_WAT := $(wildcard internal/plugins/component/testdata/wat/*.wat internal/plugins/core/testdata/wat/*.wat)
ENGINE_FIXTURES := internal/plugins/core/testdata/engines
RUST_MANIFESTS := Cargo.toml $(wildcard crates/*/Cargo.toml testdata/plugins/*/Cargo.toml)
RUST_BUILD_INPUTS := $(RUST_MANIFESTS) Cargo.lock rust-toolchain.toml
SDK_BUILD_INPUTS := $(wildcard crates/actionlint-extension-api/src/*.rs crates/actionlint-extension-api/wit/*.wit) crates/actionlint-extension-api/build.rs

.PHONY: plugins-check-native
plugins-check-native:
	cargo check --workspace --locked --all-targets
	cargo test --locked --example pipe-to-shell
	cargo test --locked -p actionlint-extension-api
	cargo test --locked -p actionlint-extension-api --all-features

.PHONY: plugin-examples plugins
plugin-examples: $(PLUGIN_FIXTURES)/pipe-to-shell.wasm $(PLUGIN_FIXTURES)/source-annotations.wasm $(PLUGIN_FIXTURES)/pinned-actions.wasm $(PLUGIN_FIXTURES)/no-sudo.wasm $(PLUGIN_FIXTURES)/insecure-download.wasm $(PLUGIN_FIXTURES)/buildkite-runners.wasm $(PLUGIN_FIXTURES)/script-engine.wasm $(PLUGIN_FIXTURES)/action-input.wasm $(PLUGIN_FIXTURES)/ruff-script-engine.wasm $(PLUGIN_FIXTURES)/ruff-action-input.wasm

plugins: plugin-examples $(PLUGIN_FIXTURES)/misbehave.wasm $(PLUGIN_FIXTURES)/oversized-descriptor-values.wasm $(PLUGIN_FIXTURES)/oversized-descriptor-bytes.wasm $(ENGINE_FIXTURES)/echo.wasm $(PLUGIN_WAT:.wat=.wasm)

$(PLUGIN_FIXTURES)/pipe-to-shell.wasm: crates/actionlint-extension-api/wit/analyzer.wit $(RUST_BUILD_INPUTS) $(SDK_BUILD_INPUTS) examples/plugins/pipe-to-shell.rs
	cargo build --locked --profile wasm --target wasm32-wasip2 --example pipe-to-shell
	install -m 644 target/wasm32-wasip2/wasm/examples/pipe_to_shell.wasm $@

$(PLUGIN_FIXTURES)/source-annotations.wasm: crates/actionlint-extension-api/wit/analyzer.wit crates/actionlint-extension-api/wit/source-annotations.wit $(RUST_BUILD_INPUTS) $(SDK_BUILD_INPUTS) examples/plugins/source-annotations.rs
	cargo build --locked --profile wasm --target wasm32-wasip2 --example source-annotations --features source-annotations-fixture --target-dir target/plugin-fixtures/source-annotations
	install -m 644 target/plugin-fixtures/source-annotations/wasm32-wasip2/wasm/examples/source_annotations.wasm $@

$(PLUGIN_FIXTURES)/pinned-actions.wasm: crates/actionlint-extension-api/wit/analyzer.wit $(RUST_BUILD_INPUTS) examples/plugins/pinned-actions.rs
	cargo build --locked --profile wasm --target wasm32-wasip2 --example pinned-actions
	install -m 644 target/wasm32-wasip2/wasm/examples/pinned_actions.wasm $@

$(PLUGIN_FIXTURES)/misbehave.wasm: crates/actionlint-extension-api/wit/analyzer.wit $(RUST_BUILD_INPUTS) $(wildcard testdata/plugins/misbehave/src/*.rs)
	cargo build --locked --profile wasm --target wasm32-wasip2 -p actionlint-plugin-misbehave
	install -m 644 target/wasm32-wasip2/wasm/actionlint_plugin_misbehave.wasm $@

# Separate target directories keep feature variants independent under make -j.
$(PLUGIN_FIXTURES)/oversized-descriptor-%.wasm: crates/actionlint-extension-api/wit/analyzer.wit $(RUST_BUILD_INPUTS) $(wildcard testdata/plugins/misbehave/src/*.rs)
	cargo build --locked --release --target wasm32-wasip2 -p actionlint-plugin-misbehave --features oversized-descriptor-$* --target-dir target/plugin-fixtures/oversized-descriptor-$*
	install -m 644 target/plugin-fixtures/oversized-descriptor-$*/wasm32-wasip2/release/actionlint_plugin_misbehave.wasm $@

$(PLUGIN_FIXTURES)/buildkite-runners.wasm: crates/actionlint-extension-api/wit/runner.wit $(RUST_BUILD_INPUTS) examples/plugins/buildkite-runners.rs
	cargo build --locked --profile wasm --target wasm32-wasip2 --example buildkite-runners
	install -m 644 target/wasm32-wasip2/wasm/examples/buildkite_runners.wasm $@

$(PLUGIN_FIXTURES)/script-engine.wasm: crates/actionlint-extension-api/wit/script-engine.wit $(RUST_BUILD_INPUTS) examples/plugins/script-engine.rs
	cargo build --locked --profile wasm --target wasm32-wasip2 --example script-engine
	install -m 644 target/wasm32-wasip2/wasm/examples/script_engine.wasm $@

$(PLUGIN_FIXTURES)/ruff-script-engine.wasm: crates/actionlint-extension-api/wit/script-engine.wit $(RUST_BUILD_INPUTS) examples/plugins/script-engine.rs
	cargo build --locked --profile wasm --target wasm32-wasip2 --example script-engine --features ruff-fixture --target-dir target/plugin-fixtures/ruff-script-engine
	install -m 644 target/plugin-fixtures/ruff-script-engine/wasm32-wasip2/wasm/examples/script_engine.wasm $@

$(PLUGIN_FIXTURES)/action-input.wasm: crates/actionlint-extension-api/wit/script-engine.wit $(RUST_BUILD_INPUTS) examples/plugins/script-engine.rs examples/plugins/action-input.metadata.json $(wildcard tools/plugin-fixture-metadata/*.go)
	cargo build --locked --profile wasm --target wasm32-wasip2 --example script-engine --features action-input-fixture --target-dir target/plugin-fixtures/action-input
	go run ./tools/plugin-fixture-metadata target/plugin-fixtures/action-input/wasm32-wasip2/wasm/examples/script_engine.wasm examples/plugins/action-input.metadata.json $@

$(PLUGIN_FIXTURES)/ruff-action-input.wasm: crates/actionlint-extension-api/wit/script-engine.wit $(RUST_BUILD_INPUTS) examples/plugins/script-engine.rs examples/plugins/ruff-action-input.metadata.json $(wildcard tools/plugin-fixture-metadata/*.go)
	cargo build --locked --profile wasm --target wasm32-wasip2 --example script-engine --features action-input-fixture,ruff-fixture --target-dir target/plugin-fixtures/ruff-action-input
	go run ./tools/plugin-fixture-metadata target/plugin-fixtures/ruff-action-input/wasm32-wasip2/wasm/examples/script_engine.wasm examples/plugins/ruff-action-input.metadata.json $@

$(PLUGIN_FIXTURES)/no-sudo.wasm: crates/actionlint-extension-api/wit/analyzer.wit examples/plugins/no-sudo/build.sh $(wildcard examples/plugins/no-sudo/*.c)
	examples/plugins/no-sudo/build.sh $(abspath $@)

$(PLUGIN_FIXTURES)/insecure-download.wasm: crates/actionlint-extension-api/wit/analyzer.wit examples/plugins/insecure-download/build.sh examples/plugins/insecure-download/go.mod $(wildcard examples/plugins/insecure-download/*.go)
	examples/plugins/insecure-download/build.sh $(abspath $@)

internal/plugins/component/testdata/wat/%.wasm: internal/plugins/component/testdata/wat/%.wat
	wasm-tools parse $< -o $@

internal/plugins/core/testdata/wat/%.wasm: internal/plugins/core/testdata/wat/%.wat
	wasm-tools parse $< -o $@

$(ENGINE_FIXTURES)/echo.wasm: $(RUST_BUILD_INPUTS) $(wildcard testdata/plugins/echo/src/*.rs)
	cargo build --locked --profile wasm --target wasm32-unknown-unknown -p actionlint-engine-echo
	install -m 644 target/wasm32-unknown-unknown/wasm/actionlint_engine_echo.wasm $@

internal/plugins/component/bindings/kjanat/actionlint/v0.2.0/host/host.gen.go: crates/actionlint-extension-api/wit/analyzer.wit crates/actionlint-extension-api/wit/source-annotations.wit internal/plugins/component/generate.go $(wildcard tools/plugin-contract/*.go)
	go generate ./internal/plugins/component

internal/plugins/component/contract.gen.go internal/plugins/component/runner.gen.go internal/plugins/component/script_engine.gen.go &: crates/actionlint-extension-api/wit/analyzer.wit crates/actionlint-extension-api/wit/runner.wit crates/actionlint-extension-api/wit/script-engine.wit $(wildcard tools/plugin-contract/*.go) tools/plugin-contract/go.mod tools/plugin-contract/go.sum
	go run -C tools/plugin-contract .

CHANGELOG.md:
	changelog-from-release > CHANGELOG.md

clean:
	rm -f ./$(TARGET) ./man/actionlint.1 ./man/actionlint.1.html ./actionlint-workflow-ast

.PHONY: all comment-cop test clean build lint fuzz man bench cov plugins CHANGELOG.md FORCE
