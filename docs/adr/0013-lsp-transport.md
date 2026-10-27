# 0013 — LSP transport: `lsp-server` + `gen-lsp-types`, synchronous loop

Status: accepted · 2026-10-27 · verified against crates.io and project Cargo.toml files (2026-10-27)

## Context

State of the Rust LSP crates:

| crate                               | version              | last release      | status                                                                           |
| ----------------------------------- | -------------------- | ----------------- | -------------------------------------------------------------------------------- |
| `tower-lsp`                         | 0.20.0               | 2023-08-11        | unmaintained, maintainer-interest issue #427 open since 2024-08 with no response |
| `tower-lsp-server` (community fork) | 0.23.0 / 0.24.0-rc.1 | 2025-12 / 2026-09 | active, used by biome (0.23.0) and oxc (0.24.0-rc.1)                             |
| `lsp-server` (rust-analyzer)        | 0.10.0               | 2026-07-16        | active, sync `crossbeam-channel` API, used by ruff                               |
| `lsp-types` (gluon-lang)            | 0.97.0               | 2024-06-04        | stale                                                                            |
| `gen-lsp-types`                     | 0.11.0               | 2026-07-28        | generated from the LSP metamodel, used by ruff and by `tower-lsp-server`         |
| `async-lsp`                         | 0.2.4                | 2026-04-24        | active, but still pins `lsp-types 0.95`                                          |

[crates.io API, github.com/ebkalderon/tower-lsp/issues/427, ruff, biome,
oxc, tower-lsp-server `Cargo.toml` on main]

## Decision

`lsp-server = "=0.10.0"` + `lsp-types = { package = "gen-lsp-types",
version = "=0.11.0" }`. Our own dispatch loop.

Reason: the salsa cancellation model (ADR-0004) is "one storage handle per
in-flight request, an edit takes `&mut db` and cancels them all". That is
a synchronous, thread-per-request shape — exactly what rust-analyzer and
ruff run on `lsp-server`. A tower/async stack adds an executor between
the request and the database for no benefit. `tower-lsp` itself is dead.

## Consequences

- Request handling: main loop reads `lsp_server::Message`, snapshots the
  db, spawns the handler on a small thread pool, writes the response.
  Cancellation on edit via `Cancelled::catch` → `RequestCancelled` reply.
- `gen-lsp-types` tracks the metamodel, so new LSP features arrive without
  waiting on a hand-written types crate.
- No `tokio` in `lsp`. If some future feature needs async I/O it gets its
  own thread, not an executor in the hot path.

## Open

- None.
