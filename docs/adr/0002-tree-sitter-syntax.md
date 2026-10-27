# 0002 — tree-sitter-github-actions is the only syntax source

Status: accepted · 2026-10-27

## Context

`tree-sitter-github-actions` (separate repo, same author) is a full YAML 1.2
grammar with `${{ }}` expressions parsed in-tree: 69 node types covering
documents, block/flow collections, scalars, anchors/aliases, comments, and
the complete expression grammar (contexts, property access, index,
function calls, operators, format strings).

## Decision

`syntax` wraps that grammar and nothing else. No second YAML parser, no
hand-written expression parser, no serde-yaml. The CST is lossless and
incremental. Typed views over it are the only syntax API other crates see.

## Consequences

- Comments, anchors, aliases, whitespace, and quoting style are preserved —
  required for formatter and precise fixes.
- Incremental reparse is free. The LSP benefits without extra work.
- Expression parsing errors appear as CST error nodes with ranges. No
  separate error path.
- Grammar bugs are fixed in the grammar repo and released. `syntax` pins an
  exact version. Local grammar work uses a gitignored `.cargo/local.toml`
  patch, never a committed `[patch]`.
- wasm32 builds need the grammar's C source compiled for wasm. Already
  supported by tree-sitter.

## Open

- Alias/anchor expansion limits (billion-laughs) live in `semantics`, not
  the grammar. Define the budget there.
