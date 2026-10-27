# 0004 — Demand-driven query engine on salsa

Status: accepted · 2026-10-27 · verified against salsa 0.28.5 (crates.io, 2026-09-24), rust-analyzer and ruff master Cargo.toml

## Context

Analysis is a DAG of derived facts over immutable inputs (ADR-0003). The
LSP needs incremental recomputation. The CLI needs to skip work no rule
asked for. Both need cancellation. The first Rust attempt hand-rolled a
scheduler that grew document-specific branches within days.

salsa 0.28.x is the current "salsa 2022" API line. rust-analyzer uses
`0.28.5` from crates.io (no fork). ruff/ty uses `0.28.5` and ships it to
the browser via `crates/ty_wasm`. 0.28.5 is a use-after-free fix for
interned values — nothing below it is acceptable. MSRV 1.95.
[crates.io/crates/salsa, rust-analyzer `Cargo.toml`, ruff `Cargo.toml`;
salsa `CHANGELOG.md`]

## Decision

`engine` is a thin layer over salsa. Pin:

```toml
salsa = { version = "=0.28.5", default-features = false, features = ["macros", "inventory", "salsa_unstable"] }
```

`rayon` off (CLI parallelism is per-document at the driver, not inside
queries). `accumulator` off — see below. This is ruff's wasm-proven
feature set. [ruff `Cargo.toml`, ruff `.github/workflows/ci.yaml`
`cargo clippy -p ty_wasm --target wasm32-unknown-unknown`]

### Shape

- One `#[salsa::db]` struct in `engine`, generic over nothing. Crates that
  own queries declare their own `trait Db: salsa::Database` supertraits.
- `source` owns the only `#[salsa::input]`: `Document { text, revision }`
  (`DocumentId` is the salsa id).
- Derived facts are `#[salsa::tracked]` fns in the owning crate (`syntax`,
  `semantics`, `rules`). `engine` declares none.
- Multi-argument tracked fns intern an argument tuple on every call.
  Keep hot queries at one salsa-struct argument. `(DocumentId, Occurrence)`
  becomes a `#[salsa::interned] OccurrenceKey`.
  [salsa book `overview`, `setup_tracked_fn.rs`]
- Value types crossing query boundaries: `'static` types need
  `PartialEq` only (for backdating). Types carrying `'db` need
  `#[derive(salsa::SalsaValue)]` + `PartialEq`. `Update` no longer exists
  (removed 0.28.0). [salsa `src/salsa_value.rs`, CHANGELOG 0.28.0]

### Diagnostics collection — not accumulators

salsa accumulators exist for exactly this, but reading them is an
untracked read: the caller re-executes every revision, and ruff turns the
feature off. [salsa `src/function/accumulated.rs`, ruff `Cargo.toml`]

Decision: rules are tracked fns returning `Arc<[Diagnostic]>`.
`rules::diagnostics(doc)` is a tracked fn that folds over a hand-written
`&'static [RuleEntry]` in `rules/src/lib.rs` (rust-analyzer style. A test
asserts every rule module is listed). `extensions` passes its loaded
components in as additional entries. `engine` never sees `Diagnostic`
(ADR-0005). The salsa `inventory` feature in the pin is salsa's own
ingredient registration, unrelated to our registry.
[rust-analyzer `ide-diagnostics/src/lib.rs`]

### Cancellation

Unwinding with `salsa::Cancelled` (`Local | PendingWrite |
PropagatedPanic`). Caught at the driver with `Cancelled::catch`.
Triggers: any `&mut db` (pending write — blocks until other handles drop),
`db.cancellation_token().cancel()` for a local handle, or
`db.trigger_cancellation()`. Long loops inside a query call
`db.unwind_if_revision_cancelled()`. Never take `&mut db` from inside a
query — that is a documented deadlock. [salsa `src/cancelled.rs`,
`src/runtime.rs`, book `tuning.md`]

LSP: one `StorageHandle` clone per in-flight request. An edit takes
`&mut db` and cancels them all.

### Memory

Default is unbounded memoisation. `#[salsa::tracked(lru = N)]` on the
heavy ones (`tree`, `workflow`, `type_env`). Eviction happens at the start
of the next revision. Interned `OccurrenceKey` uses the default
`revisions = 3` GC. [book `tuning.md`, `src/function/eviction.rs`,
`src/interned.rs`]

### Cycles

A tracked fn that can recurse into itself (`invocation_graph`, `needs`
ordering) must declare `cycle_result = …` or `cycle_fn`/`cycle_initial`.
Otherwise salsa panics. Decision: `cycle_result` returning an
`Unresolved(Cycle)` node, and the cycle is reported as a diagnostic at the
closing call site (ADR-0006). [salsa PR #603, `options.rs`]

### Budgets

Per-document time/size budgets are an input (`#[salsa::input(singleton)]
Limits`), so changing them invalidates exactly the queries that read them.

## Consequences

- Incrementality is a property of the framework, tested by re-execution
  counts, not re-implemented.
- `engine` depends on salsa and nothing else. It compiles without `model`
  (ADR-0005).
- `salsa_unstable` is required for memory reporting (`IngredientInfo`) and
  is explicitly not semver-stable. Pin exact and bump deliberately.
- Durability: config and grammar-derived tables get
  `Durability::HIGH`. Document text stays `LOW` (default).

## Open

- Whether the `rayon` feature compiles on wasm32 is unverified. It is off,
  so moot unless parallel queries are wanted later.
- Exact `Send + Sync` bound site on tracked-fn outputs unverified. Model
  types are `Send + Sync` anyway (`Arc`, no `Rc`/`RefCell`).
