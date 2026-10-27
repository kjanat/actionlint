# 0015 — Rule ids are `area/name`, category is metadata, one `[rules]` map with globs

Status: accepted · 2026-10-28 · form surveyed in ruff, biome v2, oxlint, clippy/rustc, zizmor (docs + main, 2026-10-28)

## Context

Five mature linters, five different shapes:

|        | id                | hierarchy                                  | per-rule level                                | reason required | unused-suppression report                  | stability gate              |
| ------ | ----------------- | ------------------------------------------ | --------------------------------------------- | --------------- | ------------------------------------------ | --------------------------- |
| ruff   | `F401`            | upstream-linter prefix (+preview category) | none                                          | no              | opt-in `RUF100`                            | `preview = true` hard gate  |
| biome  | `lint/group/name` | group in id, + domains                     | off/on/info/warn/error, default per rule      | **yes**         | `suppressions/unused`                      | nursery = opt-in            |
| oxlint | `plugin/name`     | plugin × category                          | off/warn/error                                | no              | opt-in                                     | nursery excluded from `all` |
| clippy | `clippy::name`    | tool × category                            | allow/expect/warn/force-warn/deny/forbid      | optional        | `expect` → `unfulfilled_lint_expectations` | nursery = allow default     |
| zizmor | `kebab-id`        | none                                       | fixed severity × confidence, `remap.severity` | optional        | none                                       | persona axis                |

[docs.astral.sh/ruff, biomejs.dev/linter, /analyzer/suppressions, oxc.rs
linter config. Rust-clippy README, rustc lint levels. Docs.zizmor.sh]

Full comparison with verbatim config and macro snippets in
`design/rules.md`'s sources. Decisions below pick per aspect.

## Decision

1. **Id = `area/name`.** Area is the model layer the rule reads
   (`schema`, `expression`, `uses`, …. Full table in `design/rules.md`).
   Extensions get their own namespace. Not biome's `group/name` because
   the group is an opinion and renaming ids breaks configs. Not ruff's
   codes because there is no upstream to encode.
2. **Category is metadata** with clippy's six names and meanings
   (`correctness`, `suspicious`, `style`, `pedantic`, `restriction`,
   `nursery`), as oxlint does. Category supplies the default level:
   `error`, `warn`, `warn`, `off`, `off`, `off`.
3. **Levels** `off | on | info | warn | error` (biome). Any rule may be
   set to any level. No clippy `forbid`/`force-warn` (no need for an
   un-overridable level in a single-project tool), no zizmor confidence
   axis.
4. **Correctness rules must cite an ADR-0014 artefact.** Enforced by a
   test over `RuleMeta.sources`. No peer does this. It is the mechanism
   that keeps "GitHub's reference" from decaying into opinion.
5. **One `[rules]` map** keyed by exact id, `area/*`, or `@category`,
   plus `preset = recommended | all | none` and `[[overrides]]` with
   `include` globs, last match wins. Precedence: override > exact >
   area glob > category glob > preset. CLI above all. No
   `select`/`ignore`/`extend-select` (ruff's three keys need a
   specificity ladder to explain their interaction).
6. **Per-rule `{ level, fix, options }`** table (biome shape). `fix ∈
   none | safe | unsafe` reclassifies applicability (ADR-0007). `options`
   typed per rule via `schemars`.
7. **Suppression** `# actionlint-ignore <ids>: <reason>` with trailing,
   own-line, `-file`, `-start`/`-end` forms. Reason mandatory (biome).
   Unused reported with a fix (biome, clippy `expect`). No blanket
   form. Ids may use the same globs as config.
8. **Stability**: `nursery` category, config-only opt-in (biome/oxlint),
   no `--preview` flag (ruff). `since`/`deprecated` metadata. Renamed and
   removed ids kept in a table for precise diagnostics (clippy, ruff).
9. **Presets**: `recommended` = `correctness` + `suspicious` + `style`.
   `all` = everything except `restriction` and `nursery` (oxlint/biome
   exclude nursery from `all`. Clippy's README says restriction must not
   be enabled as a whole).

## Consequences

- A rule's id never changes once stable. Its category may, with a minor
  bump, and that changes only its default.
- `RuleMeta` is the single declaration site. Docs, schema (`options`),
  and the registry test all derive from it.
- The `schema/*`, `expression/*`, and `preview/*` families are
  data-driven from the vendored GitHub artefacts (ADR-0014). Adding a key
  to GitHub's schema adds no code here.
- Config schema grows one entry per rule with options. `xtask schema`
  diff-check catches a rule that forgets to register its options.

## Open

- `@category` glob spelling.
- `style` default `warn` vs `info`.
- Occurrence-scoped suppression syntax (post-2.0).
