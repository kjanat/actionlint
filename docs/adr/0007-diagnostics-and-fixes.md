# 0007 — Diagnostics carry provenance, fixes are revision-pinned edits

Status: accepted · 2026-10-27

## Context

Consumers need to render, group, suppress, and apply. Each of those needs
to know exactly where a finding came from and against which text.

## Decision

```rust
struct Diagnostic {
    rule: RuleId,       // "expression/undefined-context"
    severity: Severity, // Error | Warning | Info | Hint
    message: String,
    at: SourceRef, // primary range, revision-pinned
    occurrence: OccurrenceId,
    related: Vec<(SourceRef, String)>,
    fixes: Vec<Fix>,
    origin: Origin, // Builtin | Extension(name, version)
}
struct Fix {
    title: String,
    safe: bool,
    edits: Vec<(SourceRef, String)>,
}
```

- All `SourceRef`s in one `Fix` share one revision. The CLI/LSP verifies
  before applying. Mismatch rejects the fix.
- `safe: true` means semantics-preserving by construction (formatting,
  canonicalisation). Only safe fixes run under `--fix` without `--unsafe`.
- Rule ids are `<area>/<name>`, kebab-case, documented in
  `rules/src/<area>/<name>.rs` doc comment. `xtask rules-doc` renders them.
- Severity is the rule's default. Config may lower or raise it per rule
  (ADR-0010). Extensions declare defaults in their manifest.

## Consequences

- Output formats (text/json/sarif/lsp) are pure projections. No format has
  information the others lack.
- Overlapping fixes from different rules are detected by range before
  application. Conflicts skip the later one and report.

## Open

- Fix ordering across rules when both are safe and non-overlapping: apply
  by position. Confirm no rule relies on order.
