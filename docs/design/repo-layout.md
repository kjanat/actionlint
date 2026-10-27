# Repository layout

One Cargo workspace. One crate per layer from `architecture.md`, plus
consumers. No crate exists for packaging convenience. Every crate is a
dependency boundary someone would otherwise violate.

```text
.
├── Cargo.toml                 workspace
├── rust-toolchain.toml
├── crates/
│   ├── source/                actionlint-source
│   ├── syntax/                actionlint-syntax
│   ├── model/                 actionlint-model
│   ├── semantics/             actionlint-semantics
│   ├── engine/                actionlint-engine
│   ├── rules/                 actionlint-rules
│   ├── workspace/             actionlint-workspace
│   ├── extensions/            actionlint-extensions   (host side)
│   ├── extension-sdk/         actionlint-extension-sdk (guest side, Rust)
│   ├── cli/                   actionlint   (the binary)
│   └── lsp/                   actionlint-lsp
├── wit/                       the extension contract, versioned package
├── xtask/                     schema gen, fixture regen, release checks
├── fixtures/                  shared test inputs (workflows, actions, repos)
├── tests/                     cross-crate integration + guest acceptance
│   └── guests/
│       ├── c-minimal/         C guest, source+tree only — the boundary test
│       └── rust-full/         Rust guest touching every provider
├── docs/
│   ├── design/
│   └── adr/
├── distribution/              docker, nix, npm, homebrew — no logic
├── playground/                wasm consumer, separate package.json
├── action.yml                 GitHub Action wrapper over the cli binary
└── .github/
```

## Crates

| crate           | depends on                                      | contains                                                                                           | must not contain                    |
| --------------- | ----------------------------------------------- | -------------------------------------------------------------------------------------------------- | ----------------------------------- |
| `source`        | —                                               | `DocumentId`, `Revision`, `Text`, `Position`, `Range`, `Edit`, `SourceRef`                         | anything YAML                       |
| `syntax`        | source, tree-sitter, tree-sitter-github-actions | `Tree`, incremental reparse, typed node views (`MappingNode`, `ExprNode`, …), `Expr` CST→AST       | semantic meaning, diagnostics       |
| `model`         | source                                          | all semantic types, `Diagnostic`, `Fix`, `Type`, `Context`, `Occurrence`                           | construction logic, I/O             |
| `semantics`     | syntax, model                                   | every provider that turns syntax into model or model into derived facts. `uses:` reference parsing | filesystem, network, config         |
| `engine`        | — (generic)                                     | `Db` trait, query definition macros/traits, memo, cancellation, budgets                            | any domain type                     |
| `rules`         | semantics, engine                               | built-in rules. Each rule = one module, one provider, one fixture dir                              | per-tool code, config parsing       |
| `workspace`     | semantics, engine, source                       | file loading, overlays, project discovery, `actionlint.toml`, caches, `resolve_uses` backing store | rules, output formatting            |
| `extensions`    | semantics, engine, wasmtime, wit                | component loading, capability negotiation, provider marshalling                                    | document-kind knowledge, tool names |
| `extension-sdk` | wit                                             | guest-side bindings and helpers for Rust extensions                                                | host code                           |
| `cli`           | workspace, rules, extensions                    | clap, output formats, exit codes, `--fix`                                                          | analysis logic                      |
| `lsp`           | workspace, rules, extensions                    | tower-lsp server, overlay↔revision, code actions                                                   | analysis logic                      |

`engine` has no domain dependency on purpose. It must compile and test
without `model` in the graph. If that stops being true, the engine has grown
a document kind and the change is wrong.

## Dependency check

`xtask check-layers` reads `cargo metadata` and fails if any crate depends on
a crate listed below it in the table. Runs in CI.

## Naming

- Crate dirs: short, no prefix. Package names: `actionlint-<dir>`. Binary: `actionlint`.
- Modules: one rule per file under `rules/src/<rule_id>.rs`. Rule id ==
  file name == fixture dir name.
- Generated code is not committed. WIT bindings come from `wit-bindgen`
  macros at build time. The one committed generated artifact is
  `actionlint.schema.json`, produced by `xtask schema`, diff-checked in CI.
- Fixtures: `fixtures/<area>/<case>/` with `input.yaml` (or a repo tree) and
  `expected.{json,txt}`. Nothing else.

## Workspace Cargo.toml

```toml
[workspace]
resolver = "3"
members  = ["crates/*", "xtask"]

[workspace.package]
version = "2.0.0-dev"
edition = "2024"
license = "MIT"
publish = false

[workspace.lints.rust]
unsafe_code = "forbid"

[workspace.lints.clippy]
all      = "deny"
pedantic = "warn"
```

No `[patch.crates-io]` in the committed workspace. Local grammar development
uses `.cargo/local.toml` (gitignored) `[patch]` entries. `replace`-style
redirects of real dependencies are an ADR, not a convenience.

## What is deliberately absent

- `internal/`, `tools/`, per-tool directories, `plugins/<name>/`.
- A `builtin` or `core` crate. Those names describe origin, not
  responsibility.
- Any crate whose only job is to re-export or package another.
