# 0010 — Config via serde + schemars, schema generated and diff-checked

Status: accepted · 2026-10-27

## Context

v1 hand-maintained `actionlint.schema.json` next to a Go struct. They
drifted. Editors and docs need a schema that is true by construction.

## Decision

- One config type tree in `workspace`, `#[derive(Deserialize, JsonSchema)]`
  with `#[serde(deny_unknown_fields)]`.
- File: `actionlint.toml` at project root (walk-up discovery), optional
  `.actionlint.toml` for dotfile preference. Not YAML. The tool's own
  config should not be the thing it lints.
- `xtask schema` writes `actionlint.schema.json`. CI runs it and fails on
  diff. The committed schema is the only generated artifact in the repo
  (ADR-0012).
- Rule configuration: `[rules."<area>/<name>"] severity = "warning"` plus
  rule-specific typed tables. Unknown rule ids are diagnostics, not errors.
- Extensions declare their config schema in their manifest. The host
  validates `[extensions."<name>"]` against it at load.

## Consequences

- Config errors surface as diagnostics at the config file's `SourceRef`
  — the config file is a document like any other.
- No global config access below `workspace`. Rules receive their settings
  as typed query inputs.

## Open

- Whether to support `pyproject.toml`-style embedding (`[tool.actionlint]`)
  for monorepos. Default no.
