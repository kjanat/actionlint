# GNU make's built-in `%.out: %` rule deletes an expected-output fixture and copies its input
# directory over it whenever the directory is newer, so no built-in rule may apply here.
MAKEFLAGS += --no-builtin-rules

GO_DIRS := $(shell go list -f '{{.Dir}}' ./...)
GO_FILES := $(patsubst $(CURDIR)/%,%,$(wildcard $(addsuffix /*.go,$(GO_DIRS))))
SRCS := $(filter-out %_test.go, $(GO_FILES)) internal/config/config_default.yaml internal/actioncatalog/popular_actions.json internal/webhook/payload/catalogue.json go.mod go.sum
TESTS := $(filter %_test.go, $(GO_FILES))
TOOL := $(wildcard internal/*/generate/*.go internal/*/metadata/*.go internal/release/bump-version/*.go internal/*/check-*/*.go)
TESTDATA := $(wildcard \
		testdata/examples/* \
		testdata/err/* \
		testdata/ok/* \
		testdata/config/* \
		testdata/format/* \
		testdata/projects/* \
		testdata/reusable_workflow_metadata/* \
	)
GO_GEN_SRCS := $(filter-out %_test.go,$(wildcard internal/actioncatalog/generate/*.go internal/actioncatalog/metadata/*.go)) \
				internal/actioncatalog/generate/popular_actions.json \
				internal/webhook/generate/main.go \
				internal/expression/context/generate/main.go
PANDOC := pandoc --standalone --from=markdown-smart --syntax-highlighting=none

ifeq ($(OS),Windows_NT)
	SHELL := powershell.exe
	.SHELLFLAGS := -NoProfile -ExecutionPolicy Bypass -Command
	TARGET = actionlint.exe
	TOUCH = powershell -NoProfile -ExecutionPolicy Bypass internal/ci/touch.ps1
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

internal/actioncatalog/popular_actions.json internal/webhook/webhooks.go internal/expression/context/availability.go internal/action/availability_generated.go internal/action/runtimes_generated.go: $(GO_GEN_SRCS)
ifdef SKIP_GO_GENERATE
	$(TOUCH) internal/actioncatalog/popular_actions.json internal/webhook/webhooks.go internal/expression/context/availability.go internal/action/availability_generated.go internal/action/runtimes_generated.go
else
	go generate
endif

GIT_DESCRIBE := $(shell git describe --tags 2>/dev/null)
ifneq ($(GIT_DESCRIBE),)
BUILD_LDFLAGS = -ldflags "-X actionlint.kjanat.dev/internal/buildinfo.ReleaseVersion=$(GIT_DESCRIBE)"
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

.github/actionlint-matcher.json: internal/githubaction/matcher/main.mjs internal/githubaction/matcher/object.mjs
	node ./internal/githubaction/matcher/main.mjs .github/actionlint-matcher.json

internal/githubaction/matcher/testdata/escape.txt: $(TARGET)
	./actionlint -color ./testdata/err/one_error.yaml > ./internal/githubaction/matcher/testdata/escape.txt || true
internal/githubaction/matcher/testdata/no_escape.txt: $(TARGET)
	./actionlint -no-color ./testdata/err/one_error.yaml > ./internal/githubaction/matcher/testdata/no_escape.txt || true
internal/githubaction/matcher/testdata/want.json: $(TARGET)
	./actionlint -format '{{json .}}' ./testdata/err/one_error.yaml > internal/githubaction/matcher/testdata/want.json || true

PLUGIN_FIXTURES := internal/plugins/component/testdata/plugins
PLUGIN_WAT := $(wildcard internal/plugins/component/testdata/wat/*.wat internal/plugins/core/testdata/wat/*.wat)
ENGINE_FIXTURES := internal/plugins/core/testdata/engines
RUST_PLUGIN_MANIFESTS := $(wildcard plugins/*/Cargo.toml plugins/examples/*/Cargo.toml internal/plugins/*/testdata/*/Cargo.toml)
RUST_PLUGIN_NATIVE_CHECKS := $(patsubst %/Cargo.toml,check-native-%,$(RUST_PLUGIN_MANIFESTS))

.PHONY: plugins-check-native $(RUST_PLUGIN_NATIVE_CHECKS)
plugins-check-native: $(RUST_PLUGIN_NATIVE_CHECKS)

$(RUST_PLUGIN_NATIVE_CHECKS): check-native-%: %/Cargo.toml
	cargo check --locked --all-targets --manifest-path "$<"

.PHONY: plugins
plugins: $(PLUGIN_FIXTURES)/pipe-to-shell.wasm $(PLUGIN_FIXTURES)/no-sudo.wasm $(PLUGIN_FIXTURES)/insecure-download.wasm $(PLUGIN_FIXTURES)/misbehave.wasm $(ENGINE_FIXTURES)/echo.wasm $(PLUGIN_WAT:.wat=.wasm)

$(PLUGIN_FIXTURES)/pipe-to-shell.wasm: plugins/wit/analyzer.wit plugins/examples/pipe-to-shell/Cargo.toml plugins/examples/pipe-to-shell/Cargo.lock $(wildcard plugins/examples/pipe-to-shell/src/*.rs)
	cargo build --locked --release --target wasm32-wasip2 --manifest-path plugins/examples/pipe-to-shell/Cargo.toml
	install -m 644 plugins/examples/pipe-to-shell/target/wasm32-wasip2/release/actionlint_plugin_pipe_to_shell.wasm $@

$(PLUGIN_FIXTURES)/misbehave.wasm: plugins/wit/analyzer.wit internal/plugins/component/testdata/misbehave/Cargo.toml internal/plugins/component/testdata/misbehave/Cargo.lock $(wildcard internal/plugins/component/testdata/misbehave/src/*.rs)
	cargo build --locked --release --target wasm32-wasip2 --manifest-path internal/plugins/component/testdata/misbehave/Cargo.toml
	install -m 644 internal/plugins/component/testdata/misbehave/target/wasm32-wasip2/release/actionlint_plugin_misbehave.wasm $@

$(PLUGIN_FIXTURES)/no-sudo.wasm: plugins/wit/analyzer.wit plugins/examples/no-sudo/build.sh $(wildcard plugins/examples/no-sudo/*.c)
	plugins/examples/no-sudo/build.sh $(abspath $@)

$(PLUGIN_FIXTURES)/insecure-download.wasm: plugins/wit/analyzer.wit plugins/examples/insecure-download/build.sh plugins/examples/insecure-download/go.mod $(wildcard plugins/examples/insecure-download/*.go)
	plugins/examples/insecure-download/build.sh $(abspath $@)

internal/plugins/component/testdata/wat/%.wasm: internal/plugins/component/testdata/wat/%.wat
	wasm-tools parse $< -o $@

internal/plugins/core/testdata/wat/%.wasm: internal/plugins/core/testdata/wat/%.wat
	wasm-tools parse $< -o $@

$(ENGINE_FIXTURES)/echo.wasm: internal/plugins/core/testdata/echo/Cargo.toml internal/plugins/core/testdata/echo/Cargo.lock $(wildcard internal/plugins/core/testdata/echo/src/*.rs)
	cargo build --locked --release --target wasm32-unknown-unknown --manifest-path internal/plugins/core/testdata/echo/Cargo.toml
	install -m 644 internal/plugins/core/testdata/echo/target/wasm32-unknown-unknown/release/actionlint_engine_echo.wasm $@

internal/plugins/component/bindings/kjanat/actionlint/host/host.go: plugins/wit/analyzer.wit internal/plugins/component/generate.go
	go generate ./internal/plugins/component

CHANGELOG.md:
	changelog-from-release > CHANGELOG.md

clean:
	rm -f ./$(TARGET) ./man/actionlint.1 ./man/actionlint.1.html ./actionlint-workflow-ast

.PHONY: all comment-cop test clean build lint fuzz man bench cov plugins CHANGELOG.md FORCE
