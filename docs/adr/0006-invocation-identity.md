# 0006 — Occurrence = call-site path, `uses:` resolution is cache-only

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
    `<owner>/<repo>/.github/workflows/<f>@<ref>`. `<ref>` is a SHA, tag,
    or branch name. `refs/heads/…`, `refs/tags/…` prefixes are invalid.
    Expressions are not allowed in this field.
  - `steps[*].uses` and composite `runs.steps[*].uses`:
    `<owner>/<repo>@<ref>`, `<owner>/<repo>/<path>@<ref>`, `./<path>`,
    `$/<path>`, `docker://<image>:<tag>`, `docker://<host>/<image>:<tag>`.
    A missing `@ref` on a remote action is documented as "strongly
    recommended", not forbidden — it is a rule, not a parse error.
    [docs.github.com workflow-syntax `jobs.<job_id>.uses`,
    `jobs.<job_id>.steps[*].uses`. Metadata-syntax `runs.steps[*].uses`]
- `resolve_uses(UsesRef) -> Resolved | Unresolved` is backed by
  `workspace`: local paths and same-repo workflows resolve from the file
  system. Remote refs resolve only from a local cache directory. No
  provider ever opens a socket. Populating the cache is a separate CLI
  command (`actionlint fetch`), explicitly invoked.
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
    The graph builder itself stops at a configurable depth (default 16)
    purely as a resource bound.

## Consequences

- Diagnostics in a reused definition are reported once per occurrence,
  each with its full path. Consumers may collapse for display.
- Suppressions can target an `OccurrenceId`.
- Offline is the default and deterministic. CI behaviour never depends on
  GitHub availability.

## Open

- Cycle handling: salsa `cycle_result` returns `Unresolved(Cycle)`
  (ADR-0004). The rule reports at the closing call site.
- The older documented cap of "20 unique reusable workflows per file" no
  longer appears on docs.github.com. Not enforced as a rule until it is
  documented again.
