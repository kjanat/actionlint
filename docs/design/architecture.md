# Architecture

## What actionlint is

A semantic model of GitHub Actions — workflows, actions, the expression
language, the runner/context environment — and a set of consumers that read
that model: a CLI, a language server, a formatter, a GitHub Action, a browser
playground, and third-party extensions.

The model is the product. Everything else is a view on it.

## Goals

1. **Every diagnostic at every level.** Workflow, job, step, expression,
   script, action metadata, reusable-workflow call, matrix cell. No level is
   special-cased or out of reach.
2. **One owner per fact.** A fact about a document (its jobs, an expression's
   type, which runner a step executes on) is computed in exactly one place and
   read everywhere else.
3. **Identity survives composition.** A step reached through two different
   call paths is two occurrences with two identities, each traceable to its
   outer call site.
4. **Editor-grade.** Lossless syntax, incremental re-analysis, cancellation,
   stable identities across edits. The LSP is a first consumer, not a port.
5. **Extensible without special cases.** Built-in rules and third-party
   extensions use the same provider API. The host has no knowledge of any
   specific external tool.

## Non-goals

- Behavioural compatibility with v1 (Go). Output formats may converge later.
  Internals never.
- Executing workflows.
- Shipping or bundling external linters (ShellCheck, Ruff, pyflakes). They are
  extensions, maintained outside this repository, or not at all.

## Layers

Strictly downward dependencies. A layer may depend only on layers above it in
this list.

```text
source      text, revisions, positions, ranges, edits
syntax      lossless CST (tree-sitter-github-actions), typed node views
model       semantic types: Workflow, Action, Job, Step, Expr, Type, Diagnostic
semantics   providers: syntax → model, resolution, typing, invocation graph
engine      demand-driven query runtime: memoisation, cancellation, budgets
rules       built-in analyses (consumers of semantics via engine)
workspace   files, overlays, config, project discovery, persistent cache
extensions  WIT contract + wasmtime host, exposes the same providers to guests
consumers   cli, lsp, action, playground
```

`engine` is below `rules` and `extensions` but knows nothing about YAML,
workflows, document kinds, or tool names. It schedules typed queries and
caches their results per source revision. That is all.

## Data flow

```text
bytes ──source──▶ Document@Revision
             ──syntax──▶ Tree (CST, lossless, incremental)
             ──semantics──▶ Workflow | Action (model)
             ──semantics──▶ InvocationGraph, TypeEnv, RunnerFacts, …
             ──rules / extensions──▶ Diagnostic*
             ──consumers──▶ text | json | sarif | lsp | fixes
```

Every arrow is a query. Queries are pure functions of their inputs and are
memoised by `engine` keyed on `(query, input identity, revision)`. A consumer
asks for the diagnostics of a document. The engine pulls exactly the providers
that need to run and nothing else. A rule that only reads the CST never
triggers semantic resolution.

## Identity

### Source identity

`DocumentId` — stable across the process lifetime for one logical file (path,
or editor URI, or synthetic for embedded/test content).

`Revision` — monotonically increasing per `DocumentId`. Every query result,
diagnostic, and fix is pinned to a revision. A fix against a stale revision is
rejected, not applied.

`SourceRef = (DocumentId, Revision, Range)` — the only way to point at text.

### Invocation identity

GitHub Actions composes: a workflow calls a reusable workflow which calls a
composite action which runs steps. The same step definition reached through
two call paths is two different things with potentially different inputs,
secrets, runners and failure modes.

`CallSite = SourceRef` of the `uses:` (or `jobs.<id>` for the entry).

`Occurrence = [CallSite]` — the path from the entry document to the thing.
Two occurrences are equal iff their paths are equal. The entry workflow's own
jobs have a one-element path.

`OccurrenceId` is a content hash of the path, stable across runs and
machines. Diagnostics carry it. Suppressions may target it.

The invocation graph is a provider like any other: entry document →
`InvocationGraph`. Resolution of `uses:` targets (local path, same-repo
workflow, remote `owner/repo@ref`) is a separate provider that `workspace`
backs with filesystem, cache, or nothing (offline → unresolved node, not an
error at this layer).

## Model

Minimum semantic model. Each type is immutable, owns a `SourceRef`, and is
produced by one provider.

- `Workflow { name, on, env, defaults, permissions, concurrency, jobs }`
- `Job { id, needs, runs_on, strategy, steps | uses, with, secrets, … }`
- `Step { id, uses | run, shell, with, env, if, … }`
- `Action { name, inputs, outputs, runs: Composite | Docker | Node }`
- `Expr` — parsed `${{ }}` AST, from the CST's expression nodes
- `Type` — the expression type lattice (null, bool, number, string, array,
  object, any) with context-aware object shapes
- `Context` — `github`, `env`, `secrets`, `matrix`, `needs`, `inputs`,
  `steps`, `job`, `runner`, `strategy`, `vars`
- `Diagnostic { rule, severity, message, at: SourceRef, occurrence,
  related: [SourceRef], fixes: [Fix] }`
- `Fix { title, edits: [(SourceRef, replacement)] }` — all edits against one
  revision

Unknown keys, unparsed scalars, and YAML errors do not abort model
construction. The model carries `Unknown`/`Invalid` leaves with their
`SourceRef` so rules can report them and later layers still run.

### Aliases, flow collections, folded scalars

Real workflows (this repo's `ci.yml` has 30+ anchors) reuse steps,
`with:` mappings, `env:` mappings, and `uses:` scalars via YAML
anchors/aliases, and lean on flow syntax and multi-line plain scalars.
The model therefore records, per node:

- `at: SourceRef` — where the node is *used* (the alias site, e.g.
  `- *ActCheckNocred` at line 185), so a diagnostic lands where the user
  is looking.
- `defined_at: Option<SourceRef>` — the anchor site (line 158) when the
  node came through an alias, surfaced as `related`. One anchor used in
  six jobs yields six model nodes with six `at` and one `defined_at`.
- Alias expansion is a `semantics` provider over the CST. The CST itself
  stays unexpanded (lossless). Alias budget (billion-laughs) is enforced
  there (ADR-0002 open item).

Scalars keep a **source map** from logical text to CST ranges: a folded
plain scalar (`run: a\n  b\n  c` → `a b c`) or `>-` block maps every
logical offset back to its line/column, so `script/*` and extensions
report inside the original lines, not at the scalar's first character.

## Providers

A provider is `fn(&dyn Db, Input) -> Output` where `Input` and `Output` are
`Eq + Hash + Clone` and the function is deterministic. Examples:

| provider           | input                           | output                 |
| ------------------ | ------------------------------- | ---------------------- |
| `tree`             | DocumentId                      | syntax::Tree           |
| `workflow`         | DocumentId                      | model::Workflow        |
| `action`           | DocumentId                      | model::Action          |
| `expressions`      | DocumentId                      | [Expr @ SourceRef]     |
| `type_env`         | (DocumentId, Occurrence)        | TypeEnv                |
| `runner_facts`     | (DocumentId, JobId, Occurrence) | RunnerFacts            |
| `invocation_graph` | DocumentId                      | InvocationGraph        |
| `resolve_uses`     | UsesRef                         | Resolved \| Unresolved |
| `diagnostics`      | DocumentId                      | [Diagnostic]           |

Rules and extensions are themselves providers producing `[Diagnostic]`. The
`diagnostics` provider is a fold over a rule registry. Nothing in `engine`
knows this list.

The registry is a hand-written `&'static [RuleEntry]` in `rules/src/lib.rs`,
the way rust-analyzer lists its 79 diagnostic handlers. A test asserts every
`rules/src/**/*.rs` module appears in it. No `inventory`, no proc-macro
enum, no generated `registry.rs` — all three exist in peers (ruff, oxc,
biome respectively) and each buys indirection we don't need at this size.
`extensions` contributes entries at load time through the same `RuleEntry`
type. [rust-analyzer `ide-diagnostics/src/lib.rs`, ruff `codes.rs`, biome
`xtask/codegen`]

## Extensions

Same provider API, across a WIT boundary. A guest declares which providers it
needs (`tree`, `workflow`, `type_env`, …) and which it offers
(`diagnostics`, later: `fixes`, `format`, `completions`). The host supplies
exactly the requested data, lazily, per call. The host never pushes
`jobs`/`expressions`/anything unrequested.

A guest that needs only `source` and `tree` works against any document the
host can parse. That is the acceptance test for the boundary: a C guest with
zero workflow knowledge produces a diagnostic without any host change.

Built-in rules are compiled into the binary and call providers natively. They
are not privileged. They use the same API an extension gets, minus the
serialization.

## Consumers

- **cli** — `actionlint` bare prints help, exit 0. `actionlint check [paths]`
  exits 0 clean, 1 findings, 2 usage/config error, 3 nothing to analyse.
  Output: text, json, sarif. Fixes: `--fix` applies all safe fixes at the
  revision they were computed for.
- **lsp** — overlays map to `(DocumentId, Revision)`. Diagnostics push on
  change. Code actions are `Fix`es. Hover/completion read `type_env`.
- **action** — thin wrapper around cli. Problem-matcher output.
- **playground** — wasm32 build of `syntax`+`semantics`+`rules` plus
  `workspace` with its `os` feature off (same pattern as ruff's
  `ruff_db`/`ruff_wasm` and taplo's `WasmEnvironment`). No extensions.

## Testing

- `syntax`: corpus tests from the grammar repo. Snapshot of typed views.
- `semantics`: golden model per fixture document. Property: re-parse after
  any single-character edit yields a model whose diffs are confined to the
  edited node's subtree.
- `engine`: query re-execution counts under edits (incrementality is tested,
  not assumed).
- `rules`: one fixture dir per rule, `.yaml` + `.expected` diagnostics,
  keyed by `OccurrenceId` where composition is involved.
- `extensions`: the C guest acceptance test above. A Rust guest exercising
  every provider. Negative tests for unrequested-data leaks.
- Integration: composed fixtures (workflow → reusable → composite) asserting
  distinct occurrences and correct outer call sites.

## Configuration

One `actionlint.toml` (project) plus editor-provided overrides. `serde` +
`schemars`. JSON schema generated by `xtask`, committed, CI fails on drift.
Config is read by `workspace`. Nothing below `workspace` sees it. Rule
settings reach rules as typed provider inputs, not as a global.

## Prior art

Surveyed 2026-10-27 on main/master: ruff (54 crates), biome (108), oxc
(44), rust-analyzer (36), taplo (6).

| aspect            | peers                                                                                               | here                                         | why differ                                                                                                               |
| ----------------- | --------------------------------------------------------------------------------------------------- | -------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| syntax            | rowan (biome, r-a, taplo), custom AST + trivia (ruff, oxc), nobody uses tree-sitter                 | tree-sitter                                  | grammar already exists and is incremental, cost is writing typed views by hand where rowan users codegen them (ADR-0002) |
| query runtime     | salsa 0.28.5 (ruff/ty, biome, r-a), none (oxc, taplo)                                               | salsa 0.28.5                                 | —                                                                                                                        |
| db input          | biome: parsed tree is the `#[salsa::input]`, ruff/r-a: text                                         | text                                         | reparse is a query so edits are cheap and the tree is evictable                                                          |
| fix model         | ruff `Applicability` tri-state, r-a `SourceChange` by `FileId`                                      | ruff's tri-state + explicit revision         | ADR-0007                                                                                                                 |
| rule layout       | one file per rule, everywhere                                                                       | same                                         | —                                                                                                                        |
| rule registry     | ruff proc-macro enum, biome/r-a xtask codegen, oxc `lintgen`                                        | hand-listed + test                           | smallest thing that works                                                                                                |
| layer enforcement | none automated anywhere, prose only                                                                 | `xtask check-layers`                         | cheap, and the thing we most need to not lose                                                                            |
| LSP               | r-a/ruff `lsp-server`, biome/oxc `tower-lsp-server`                                                 | `lsp-server`                                 | ADR-0013                                                                                                                 |
| plugins           | ruff none, biome GritQL + JS (`AnalyzerPlugin::requires_semantic_model`), oxc JS via serialized AST | WIT components, typed per-provider imports   | ADR-0009                                                                                                                 |
| wasm              | ruff/biome/taplo via `wasm-bindgen` with fs features off, oxc via napi-wasi                         | `wasm-bindgen`, `workspace` `os` feature off | —                                                                                                                        |

## Open questions

See the end of each ADR. Collected here:

- wasm32 route for tree-sitter (vanilla crate, not arborium's fork) is
  unproven until `syntax` builds for that target in CI (ADR-0002).
- Remote `uses:` resolution: fetch at all, or only consult a local cache the
  user populates? → ADR-0006 says cache-only by default.
- Formatter: in scope for v2.0 or v2.x? Layering already allows it.
