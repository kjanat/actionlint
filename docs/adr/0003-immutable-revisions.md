# 0003 — Immutable documents keyed by `(DocumentId, Revision)`

Status: accepted · 2026-10-27

## Context

Editor sessions, `--fix`, and multi-document analysis all need to say
"this result is about that text". Mutable buffers make that ambiguous and
were a recurring source of stale-fix bugs in v1.

## Decision

- `DocumentId`: process-stable identity for one logical document.
- `Revision`: monotonic per document. Every edit bumps it.
- Text behind a `(DocumentId, Revision)` never changes.
- `SourceRef = (DocumentId, Revision, Range)` is the only text pointer type.
- Every provider input and output, diagnostic, and fix carries the revision
  it was computed against.

## Consequences

- Fix application checks revision equality first. Mismatch → reject with a
  diagnostic, never silently rebase.
- Memoisation keys are trivially correct (ADR-0004).
- Memory: old revisions are dropped once no query references them. The
  engine owns that, not consumers.

## Open

- Synthetic documents (embedded test content, playground) get ids from a
  separate namespace. Exact scheme decided in `source`.
