# 0004 — Demand-driven query engine on salsa

Status: accepted · 2026-10-27

## Context

Analysis is a DAG of derived facts over immutable inputs (ADR-0003). The
LSP needs incremental recomputation. The CLI needs to skip work no rule
asked for. Both need cancellation. The first Rust attempt hand-rolled a
scheduler that grew document-specific branches within days.

## Decision

`engine` is a thin layer over `salsa` (the rust-analyzer query framework):

- `Db` trait with input queries (`text(DocumentId) -> Text`) and derived
  queries (`tree`, `workflow`, `type_env`, …) declared in the crates that
  own them, not in `engine`.
- `engine` adds: cancellation via salsa's `Cancelled` unwinding, per-query
  budgets (time/size) as inputs, and a `register_diagnostics_provider`
  hook used by `rules` and `extensions`.
- Memo keys are `(query, args)`. Args always include a revision or a type
  that embeds one.

## Consequences

- Incrementality is a property of the framework, tested by re-execution
  counts, not re-implemented.
- All query inputs/outputs must be `Eq + Hash + Clone`. Model types are
  designed for that (`Arc` internals, no interior mutability).
- Adds a dependency with its own release cadence. Pin exact.

## Open

- wasm32 (playground): salsa uses threads optionally. Verify the
  single-threaded feature set builds. If not, `engine` keeps the same API
  over a minimal memo and this ADR is superseded.
