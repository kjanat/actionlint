# 0006 — Occurrence = call-site path, `uses:` resolution is cache-only

Status: accepted · 2026-10-27

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
- `resolve_uses(UsesRef) -> Resolved | Unresolved` is backed by
  `workspace`: local paths and same-repo workflows resolve from the file
  system. Remote refs resolve only from a local cache directory. No
  provider ever opens a socket. Populating the cache is a separate CLI
  command (`actionlint fetch`), explicitly invoked.

## Consequences

- Diagnostics in a reused definition are reported once per occurrence,
  each with its full path. Consumers may collapse for display.
- Suppressions can target an `OccurrenceId`.
- Offline is the default and deterministic. CI behaviour never depends on
  GitHub availability.

## Open

- Cycle handling in the graph: report as diagnostic at the closing call
  site and truncate. Confirm GitHub's own limit for nesting depth and
  mirror it.
