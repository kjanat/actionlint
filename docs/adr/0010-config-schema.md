# 0010 — Config via serde + schemars, schema generated and diff-checked

Status: accepted · 2026-10-27 · verified against schemars 1.2.3 (crates.io, 2026-10-12)

## Context

v1 hand-maintained `actionlint.schema.json` next to a Go struct. They
drifted. Editors and docs need a schema that is true by construction.

## Decision

- One config type tree in `workspace`, `#[derive(Deserialize, JsonSchema)]`
  with `#[serde(deny_unknown_fields)]`. schemars maps that to
  `additionalProperties: false`, so editors reject the same keys the
  parser does. [graham.cool/schemars/deriving/attributes]
- Schema draft pinned explicitly: `SchemaSettings::draft2020_12()`. That is
  today's default but documented as subject to change.
  [docs.rs/schemars `SchemaSettings`]
- No `#[serde(flatten)]` in the config tree. Flattened structs under
  `deny_unknown_fields` produce schemas that need `unevaluatedProperties`
  and have a history of unsatisfiable output (schemars #325, fixes through
  1.1.0). Nest tables explicitly instead. `#[serde(with = …)]` fields
  need a matching `#[schemars(with = "…")]`.
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
