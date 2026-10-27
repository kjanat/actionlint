# 0002 — tree-sitter-github-actions is the only syntax source

Status: accepted · 2026-10-27 · verified against tree-sitter 0.27.1 (crates.io, 2026-10-08)

## Context

`tree-sitter-github-actions` (separate repo, same author) is a full YAML 1.2
grammar with `${{ }}` expressions parsed in-tree: 69 node types covering
documents, block/flow collections, scalars, anchors/aliases, comments, and
the complete expression grammar (contexts, property access, index,
function calls, operators, format strings).

No comparable Rust linter uses tree-sitter: biome, rust-analyzer and
taplo use rowan (lossless CST with code-generated typed nodes). Ruff and
oxc use hand-written ASTs with trivia kept separately. This ADR is a
deliberate deviation. [survey of each project's `Cargo.toml`, 2026-10-27]

## Decision

`syntax` wraps that grammar and nothing else. No second YAML parser, no
hand-written expression parser, no serde-yaml. The CST is lossless and
incremental. Typed views over it are the only syntax API other crates see.

## Consequences

- Comments, anchors, aliases, whitespace, and quoting style are preserved —
  required for formatter and precise fixes.
- Cost of the deviation: rowan users get typed node wrappers generated
  from a grammar description (biome `xtask/codegen`, rust-analyzer
  `syntax` codegen). Here the typed views over tree-sitter nodes are
  written by hand in `syntax`, driven by `node-types.json` from the
  grammar crate. 69 node types is tractable. A small `xtask` generator
  from `node-types.json` is the fallback if it is not (and would be
  committed-free per ADR-0012).
- Incremental reparse is free. The LSP benefits without extra work.
- Expression parsing errors appear as CST error nodes with ranges. No
  separate error path.
- Grammar bugs are fixed in the grammar repo and released. `syntax` pins an
  exact version. Local grammar work uses a gitignored `.cargo/local.toml`
  patch, never a committed `[patch]`.
- Pin `tree-sitter = "=0.27.1"`, `default-features = false`,
  `features = ["std"]`. Do **not** enable the `wasm` feature: it means
  "load grammars that were compiled to wasm, via wasmtime" (host-side
  `WasmStore`), not "build the runtime for wasm". [tree-sitter
  `lib/binding_rust/README.md`. `lib/Cargo.toml`]
- Playground (wasm32-unknown-unknown): the Rust runtime + grammar C source
  compile to that target since PR #4820 (2025-09, 0.26) with the
  `js-loader` feature and a `wasm-bindgen` post-processing step. Threads
  supported since 0.27.0 (#5851). Alternative is `web-tree-sitter` + a
  grammar `.wasm` from `tree-sitter build --wasm`, which would duplicate
  the parse pipeline in JS. Decision: Rust route, so `syntax`/`semantics`/
  `rules` run unchanged in the browser. Only known production example is
  bearcove/arborium, which uses a patched tree-sitter fork — treat the
  vanilla-crate route as **unproven until the playground builds**.
  [tree-sitter PR #4820, PR #5851, release v0.27.0, docs.rs/arborium-html]

## Open

- Alias/anchor expansion limits (billion-laughs) live in `semantics`, not
  the grammar. Define the budget there.
- wasm32 route is a milestone-1 risk: build `syntax` for
  wasm32-unknown-unknown in CI from the first commit of that crate, before
  any playground work.
