# 0006 — Occurrence = call-site path, `uses:` resolution is networked, offline degrades

Status: accepted · 2026-10-27 · limits and `uses:` forms verified against docs.github.com (2026-10-27)

## Context

Workflows call reusable workflows. Jobs use composite actions. Composite
actions use other actions. The same definition reached via two paths has
different inputs, secrets, runner, and `if` context. v1 lost that: two
nested invocations collapsed onto one identity and reported against the
wrong outer call site.

Resolving `uses: owner/repo@ref` needs network. Analysis must not.

## Decision

- `CallSite = SourceRef` of the `uses:` scalar (entry jobs: the `jobs.<id>`
  key).
- `Occurrence = Vec<CallSite>`, root first. Equality is path equality.
- `OccurrenceId = blake3(canonical path)`. Stable across runs/machines
  given identical sources.
- `invocation_graph(DocumentId) -> InvocationGraph` is a provider. Nodes
  are occurrences, edges are call sites. Unresolvable targets are
  `Unresolved(UsesRef, reason)` nodes, not errors.
- `UsesRef` is parsed in `semantics` from the documented forms, by
  position:
  - `jobs.<id>.uses` (reusable workflow): `./.github/workflows/<f>`,
    `$/.github/workflows/<f>` (same repo, no `@ref`, not on GHES),
    `<owner>/<repo>/.github/workflows/<f>@<ref>`. Expressions are not
    allowed in this field.
  - `steps[*].uses` and composite `runs.steps[*].uses`:
    `<owner>/<repo>@<ref>`, `<owner>/<repo>/<path>@<ref>`, `./<path>`,
    `$/<path>`, `docker://<image>:<tag>`, `docker://<host>/<image>:<tag>`.
    A missing `@ref` on a remote action is documented as "strongly
    recommended", not forbidden — it is a rule, not a parse error.
    [docs.github.com workflow-syntax `jobs.<job_id>.uses`,
    `jobs.<job_id>.steps[*].uses`. Metadata-syntax `runs.steps[*].uses`]
- **What `<ref>` may be is consumer-specific and measured, not
  documented** (ADR-0017, commitish). The ref is kept byte-exact: no
  case folding, no Unicode normalisation (NFC and NFD spellings select
  different content). Classification goes through
  `commitish::RefForm::classify` with `Profile::GithubAction` or
  `Profile::GithubWorkflow`:
  - actions accept `^`, `~1`, `^{}`, `^{commit}`, `refs/heads/…`,
    `refs/tags/…`, `HEAD`, uppercase SHA-1, and an annotated-tag object
    SHA. Reusable workflows reject every one of those at dispatch
  - reusable workflows accept `@` inside a branch name. Actions reject it
  - `tags/x` selects tag `x` for an action but the literal branch
    `tags/x` for a reusable workflow
  - abbreviated object IDs (4/12/39 chars) are rejected by both. A
    short hex spelling is a branch/tag name first
    `uses/ref-rejected-by-consumer` (correctness) reports a spelling the
    consumer at that position refuses.
- `./<path>` and `$/<path>` are **not** the same thing inside a remote
  composite action: `./` resolves against the runner workspace (whatever
  the caller checked out), `$/` against the action's own source
  repository. `resolve_uses` models both roots. [commitish `NESTING.md`]
- A `./.github/workflows/<f>` reference inside a *called* workflow
  resolves against the callee's repository and commit, not the root
  checkout. `invocation_graph` carries the callee's identity down.
- `resolve_uses(UsesRef) -> Resolved | Unresolved` is backed by
  `workspace`: local paths and same-repo workflows resolve from the file
  system. Remote refs resolve **over the network through the `Host`
  registry** (ADR-0018), with a content-addressed cache in front keyed
  by `(host, repo-id, sha)`. Networking is part of the tool, not an
  add-on: ref → sha, manifest, README, default branch are fetched when a
  rule or the LSP needs them, within per-run budgets (timeouts,
  concurrency, byte caps — v1's artifact limits are the starting
  numbers).
- **Offline degrades, never fails.** `--offline`, no route, a host that
  times out, or a host with no `[hosts]` entry all yield
  `Unresolved(unavailable: offline | not-fetched | lookup-disabled)` for
  that reference (v1's outcome set). Rules that need the fetched fact
  skip or downgrade for that node. Nothing that was answerable from the
  filesystem or the cache changes. A run with every host unreachable
  still exits by its findings, not by a transport error (exit 4 is for
  internal failure, not for "GitHub is down").
- The cache is the salsa-side boundary: a fetch result enters the db as
  an input keyed by `(host, repo-id, sha)`, so re-analysis is pure.
  `actionlint fetch` pre-warms it (CI images, air-gapped prep) and is
  not required for normal use.
- Depth and fan-out mirror GitHub's documented limits as rules, not as
  hard stops in the graph builder:
  - reusable workflows: 10 levels total (caller + 9 nested). Loops are
    rejected by GitHub. [reusing-workflows "Nesting reusable workflows"]
  - composite actions: up to 10 nested in one workflow.
    [avoiding-duplication comparison table]
  - a composite action cannot call a reusable workflow (reusable
    workflows are job-level, composite actions are step-level).
    [avoiding-duplication]
  - matrix: max 256 jobs per workflow run. [workflow-syntax, limits]
  - `needs`: no documented count limit. [workflow-syntax]
    The graph builder itself carries v1's per-entry budget as a resource
    bound: `max_depth 10`, `max_calls 10000`, `max_scripts 10000`. The
    first exhaustion is recorded as `Stop{parent, site, limit, max}` and
    the graph is flagged `incomplete` (v1 `invocation/graph.go:100,455`).
- Node outcomes are v1's closed set, adopted verbatim: `expanded |
  opaque(dockerfile-only, non-composite) | unavailable(non-local,
  not-fetched, lookup-disabled, placement-uncertain) |
  unresolved(invalid-call, missing-manifest, read-failed, parse-failed,
  mapping-failed) | dynamic(expression) | cycle | depth-limit |
  interrupted(canceled, operational-failure)`, reasons validated per kind
  (v1 `invocation/outcome.go:122`). Call condition is `enabled | skipped |
  runtime-dependent`.

## Consequences

- Diagnostics in a reused definition are reported once per occurrence,
  each with its full path. Consumers may collapse for display.
- Suppressions can target an `OccurrenceId`.
- Deterministic for a given cache state. CI that wants reproducibility
  pre-warms with `actionlint fetch` and runs `--offline`. CI that wants
  freshness lets it fetch. Either way GitHub being down produces
  `unavailable` nodes, not a failed run.

## Open

- Cycle handling: salsa `cycle_result` returns `Unresolved(Cycle)`
  (ADR-0004). The rule reports at the closing call site.
- The older documented cap of "20 unique reusable workflows per file" no
  longer appears on docs.github.com. Not enforced as a rule until it is
  documented again.
