# Task definitions and freshness checks live in tasks.toml.
MAKEFLAGS += --no-builtin-rules
.DEFAULT_GOAL := all
MISE_FORCE = $(if $(findstring B,$(firstword $(MAKEFLAGS))),--force)

export COMMENT_COP_BASE FUZZ_FUNC NORACE SKIP_GO_GENERATE
export MISE_TASK_RUN_AUTO_INSTALL := false
ifneq ($(origin RACE),undefined)
export RACE
endif
ifneq ($(origin PANDOC),undefined)
export PANDOC
endif

# Do not try to regenerate this compatibility entry point.
Makefile: ;

.PHONY: FORCE
FORCE:

%: FORCE
	mise run --skip-tools $(MISE_FORCE) "$@"

actionlint actionlint.exe: FORCE
	mise run --skip-tools $(MISE_FORCE) build

internal/webhook/payload/catalogue.json: FORCE
	mise run --skip-tools $(MISE_FORCE) generate:payload

internal/webhook/webhooks.gen.go internal/expression/context/availability.gen.go internal/action/availability.gen.go internal/action/runtimes.gen.go: FORCE
	mise run --skip-tools $(MISE_FORCE) generate:go

internal/plugins/component/bindings/kjanat/actionlint/host/host.gen.go: FORCE
	mise run --skip-tools $(MISE_FORCE) bindings

internal/plugins/component/contract.gen.go internal/plugins/component/runner.gen.go internal/plugins/component/script_engine.gen.go internal/plugins/component/script_engine_03.gen.go: FORCE
	mise run --skip-tools $(MISE_FORCE) contract

internal/plugins/component/testdata/wat/%.wasm: FORCE
	mise run --skip-tools $(MISE_FORCE) plugins:wat

internal/plugins/core/testdata/wat/%.wasm: FORCE
	mise run --skip-tools $(MISE_FORCE) plugins:wat
