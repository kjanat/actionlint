# GNU make's built-in `%.out: %` rule deletes an expected-output fixture and copies its input
# directory over it whenever the directory is newer, so no built-in rule may apply here.
MAKEFLAGS += --no-builtin-rules

SRCS := $(filter-out %_test.go, $(wildcard *.go cmd/*/*.go)) cmd/actionlint-action/sarif_template.txt go.mod go.sum
TESTS := $(filter %_test.go, $(wildcard *.go cmd/*/*.go))
TOOL := $(wildcard scripts/*/*.go)
TESTDATA := $(wildcard \
		testdata/examples/* \
		testdata/err/* \
		testdata/ok/* \
		testdata/config/* \
		testdata/format/* \
		testdata/projects/* \
		testdata/reusable_workflow_metadata/* \
	)
GO_GEN_SRCS := scripts/generate-popular-actions/main.go \
				scripts/generate-popular-actions/popular_actions.json \
				scripts/generate-webhook-events/main.go \
				scripts/generate-availability/main.go
PANDOC := pandoc --standalone --from=markdown-smart --syntax-highlighting=none

ifeq ($(OS),Windows_NT)
	SHELL := powershell.exe
	.SHELLFLAGS := -NoProfile -ExecutionPolicy Bypass -Command
	TARGET = actionlint.exe
	TOUCH = powershell -NoProfile -ExecutionPolicy Bypass scripts/touch.ps1
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


all: build test lint

t test:
	go test $(RACE) ./...

coverage.out: $(TESTS) $(SRCS) $(TESTDATA) $(TOOL)
	go test $(RACE) -coverprofile coverage.out -covermode=atomic ./...

coverage.html: coverage.out
	go tool cover -html=coverage.out -o coverage.html

cov: coverage.out coverage.html
	go tool cover -func=coverage.out

l lint:
	golangci-lint run
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...
ifneq ($(OS),Windows_NT)
	GOOS=js GOARCH=wasm golangci-lint run ./playground
	go run ./scripts/check-checks -quiet ./docs/checks.md
endif

popular_actions.go all_webhooks.go availability.go: $(GO_GEN_SRCS)
ifdef SKIP_GO_GENERATE
	$(TOUCH) popular_actions.go all_webhooks.go availability.go
else
	go generate
endif

GIT_DESCRIBE := $(shell git describe --tags 2>/dev/null)
ifneq ($(GIT_DESCRIBE),)
BUILD_LDFLAGS = -ldflags "-X actionlint.kjanat.dev.version=$(GIT_DESCRIBE)"
endif

$(TARGET): $(SRCS)
ifeq ($(OS),Windows_NT)
	go build $(BUILD_LDFLAGS) ./cmd/actionlint
else
	CGO_ENABLED=0 go build $(BUILD_LDFLAGS) ./cmd/actionlint
endif

b build: $(TARGET)

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
	go test -bench Lint -benchmem

.github/actionlint-matcher.json: scripts/generate-actionlint-matcher/object.mjs
	node ./scripts/generate-actionlint-matcher/main.mjs .github/actionlint-matcher.json

scripts/generate-actionlint-matcher/testdata/escape.txt: $(TARGET)
	./actionlint -color ./testdata/err/one_error.yaml > ./scripts/generate-actionlint-matcher/testdata/escape.txt || true
scripts/generate-actionlint-matcher/testdata/no_escape.txt: $(TARGET)
	./actionlint -no-color ./testdata/err/one_error.yaml > ./scripts/generate-actionlint-matcher/testdata/no_escape.txt || true
scripts/generate-actionlint-matcher/testdata/want.json: $(TARGET)
	./actionlint -format '{{json .}}' ./testdata/err/one_error.yaml > scripts/generate-actionlint-matcher/testdata/want.json || true

CHANGELOG.md:
	changelog-from-release > CHANGELOG.md

c clean:
	rm -f ./$(TARGET) ./man/actionlint.1 ./man/actionlint.1.html ./actionlint-workflow-ast

.PHONY: all test clean build lint fuzz man bench cov b t c l CHANGELOG.md
