# 0005 — The engine knows no document kind, tool, or YAML

Status: accepted · 2026-10-27

## Context

Both prior implementations put `WorkflowAnalyzer`, `Input.workflow`, and
mandatory `jobs`/`expressions` into the scheduler. The host then decided
what any analysis could see, and every new document kind or extension need
became a special case in the core.

## Decision

`engine` has zero dependencies on `model`, `syntax`, or `semantics`. It
compiles and passes its tests with none of them in the dependency graph
(`xtask check-layers` enforces this).

There is no `DocumentKind` enum anywhere below `semantics`. Whether a
document is a workflow or an action is itself a derived query
(`classify(DocumentId)`), and only providers that need the answer ask.

## Consequences

- Adding a document kind (e.g. `dependabot.yml`, `CODEOWNERS`) is new
  providers + rules, no engine change.
- An extension that needs only `tree` runs on any parseable document.
- Shared preparation ("parse once, many consumers") is a consequence of
  memoisation, not of a pre-built fact bundle.

## Open

- None.
