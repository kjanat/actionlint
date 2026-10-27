# Rules, categories, configuration

What a rule is, where it lives, what it may be based on, and how a user
turns it on, off, or down. Form decisions are in ADR-0015. The evidence
basis is ADR-0014.

## Two axes, one id

A rule id is `<area>/<name>`, kebab-case. The **area** is the layer of the
model the rule reads, not a judgement about the finding. The **category**
is the judgement, carried as metadata, and it decides the default level.

```
id        = area "/" name          e.g. expression/unknown-context
category  ∈ correctness | suspicious | pedantic | style | restriction | nursery
level     ∈ off | on | info | warn | error        ("on" = the rule's default)
```

Clippy/oxlint use the same category set with the same meanings. Biome puts
the group in the id instead. We don't, because "is this a style issue" is
an opinion that changes, and renaming ids breaks every config and
suppression comment. The area is a fact about the code and stays put.

### Areas

Each area maps to one provider family (`architecture.md`). A rule in area
X may read providers at or above X in the layer list, never below.

| area          | reads                                                      | examples                                                                                                                                                                                                                       |
| ------------- | ---------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `syntax`      | CST                                                        | `syntax/yaml-error`, `syntax/tab-indent`                                                                                                                                                                                       |
| `schema`      | CST + vendored template schema                             | `schema/unexpected-key`, `schema/missing-required`, `schema/type-mismatch`, `schema/ambiguous-mapping`, `schema/duplicate-key`, `schema/expression-not-allowed`, `schema/sequence-not-expected`, `schema/mapping-not-expected` |
| `expression`  | `expressions` provider, `context` arrays, `type_env`       | `expression/syntax`, `expression/unknown-context`, `expression/unknown-function`, `expression/arity`, `expression/depth`, `expression/length`, `expression/type-mismatch`, `expression/fromjson-lenient`                       |
| `events`      | `workflow.on`, vendored `webhooks.json`                    | `events/unknown-event`, `events/unknown-activity-type`, `events/filter-not-supported`                                                                                                                                          |
| `permissions` | `workflow`/`job` permissions                               | `permissions/unknown-scope`, `permissions/invalid-level`                                                                                                                                                                       |
| `runner`      | `runs_on`, `runner_facts`, vendored `runner-images` labels | `runner/unknown-label`, `runner/label-conflict`, `runner/deprecated-image`                                                                                                                                                     |
| `uses`        | `UsesRef`, `resolve_uses`                                  | `uses/invalid-ref`, `uses/unresolved`, `uses/unpinned-ref`, `uses/ref-prefix`, `uses/pin-comment-mismatch`, `uses/ghes-incompatible`                                                                                           |
| `workflow`    | `model::Workflow` top level                                | `workflow/missing-name`, `workflow/missing-concurrency`, `workflow/legacy-config-file`                                                                                                                                         |
| `job`         | `model::Job`                                               | `job/missing-timeout`, `job/unreachable-if`, `job/empty-steps`                                                                                                                                                                 |
| `step`        | `model::Step`                                              | `step/missing-name`, `step/run-and-uses`, `step/unused-id`                                                                                                                                                                     |
| `call`        | `invocation_graph`, `workflow_call`                        | `call/unknown-input`, `call/missing-required-input`, `call/unknown-secret`, `call/nesting-depth`, `call/cycle`                                                                                                                 |
| `action`      | `model::Action`                                            | `action/unknown-using`, `action/missing-main`, `action/missing-image`, `action/missing-steps`, `action/missing-runs`                                                                                                           |
| `matrix`      | `strategy.matrix`                                          | `matrix/include-key-not-in-matrix`, `matrix/size-limit`                                                                                                                                                                        |
| `needs`       | `job.needs` graph                                          | `needs/unknown-job`, `needs/cycle`, `needs/self`                                                                                                                                                                               |
| `env`         | `env` mappings                                             | `env/invalid-name`, `env/reserved-prefix`                                                                                                                                                                                      |
| `script`      | `scripts` provider                                         | `script/deprecated-command`, `script/unknown-shell`, `script/shell-template-no-placeholder`                                                                                                                                    |
| `preview`     | vendored schema symmetric difference (ADR-0014)            | `preview/parallel-step`, `preview/wait-step`, `preview/step-background`, `preview/snapshot`, `preview/on-relaxed`                                                                                                              |
| `suppression` | suppression comments                                       | `suppression/unused`, `suppression/unknown-rule`, `suppression/missing-reason`                                                                                                                                                 |
| `<extension>` | whatever its WIT imports grant                             | `shellcheck/sc2086` — namespaced by extension name, never colliding with core areas                                                                                                                                            |

Severity notes forced by this repo's own workflows:

- `uses/unresolved` for `./path` is `suspicious`, not `correctness`:
  `ci.yml` does `uses: ./.actionlint-action-test` on a directory a
  previous step creates. The user suppresses with a reason. The rule
  must not be an error.
- `preview/parallel-step` and `preview/concurrency-queue` fire on this
  repo's own CI and release workflows (`parallel:` groups, `queue: max`).
  Default `off` in category `nursery` until GitHub documents them. This
  repo's `actionlint.toml` sets `"preview/*" = "info"`.
- `runner/unknown-label` evaluates expression-valued `runs-on`
  (`${{ cond && matrix.os || 'ubuntu-slim' }}`) by enumerating the
  matrix values and string literals. Unknown-shape expressions are not
  reported.

Nothing in this table names an external tool. `script/*` exposes facts
about scripts. Running a linter on them is an extension (ADR-0008).

### Categories and defaults

| category      | meaning (clippy wording, adopted)                                                | default level | in `recommended` | in `all` |
| ------------- | -------------------------------------------------------------------------------- | ------------- | ---------------- | -------- |
| `correctness` | outright wrong or useless, GitHub rejects it or runs it differently than written | `error`       | yes              | yes      |
| `suspicious`  | most likely wrong or useless                                                     | `warn`        | yes              | yes      |
| `style`       | should be written in a more idiomatic way                                        | `warn`        | yes              | yes      |
| `pedantic`    | rather strict or occasional false positives                                      | `off`         | no               | yes      |
| `restriction` | prevents use of a feature, never enable as a whole                               | `off`         | no               | no       |
| `nursery`     | still under development, may change or vanish without a major bump               | `off`         | no               | no       |

**Evidence rule.** A `correctness` rule must cite at least one ADR-0014
artefact in its metadata (`sources`): a schema definition name, a runner
or language-service file + symbol, or a vendored data file. A test in
`rules` fails otherwise. `suspicious` and below may cite docs.github.com
or nothing. That is the enforced answer to "waar baseren we ze op".

## Where rules live

```
crates/rules/src/
  lib.rs                 RULES: &[RuleEntry]  — hand-listed; test asserts completeness
  schema/                one module per rule, file name == rule name
    unexpected_key.rs
    …
  expression/
  …
fixtures/rules/<area>/<name>/
  <case>.yaml            input (or a directory for multi-file cases)
  <case>.expected.json   diagnostics, keyed by OccurrenceId where composition matters
```

`schema/*` and `expression/*` are **not** hand-written per key or per
function. One validator walks the CST against the vendored template schema
and emits the `schema/*` ids. One walks expressions against the `context`
arrays and emits `expression/*`. Their messages mirror the runner's strings
(ADR-0014) so the user reads locally what GitHub would say.

Rule metadata, declared once per rule:

```rust
pub struct RuleMeta {
    pub id: &'static str, // "expression/unknown-context"
    pub category: Category,
    pub fix: FixAvailability, // None | Sometimes | Always
    pub since: &'static str,  // "2.0.0"
    pub deprecated: Option<&'static str>,
    pub sources: &'static [Source], // Schema("job-if") | Runner("ActionManifestManager.cs#ConvertRuns") | Docs(url)
    pub docs: &'static str,         // the module's doc comment: What / Why / Example / Options
}
```

`xtask rules-doc` renders `docs` + `sources` into the published rule
pages. Nothing is hand-maintained twice.

## Configuration

`actionlint.toml` (ADR-0010). Rule-related part:

```toml
[rules]
preset                       = "recommended"                                                             # recommended | all | none  (default: recommended)
"schema/*"                   = "error"                                                                   # glob on id; area glob or exact id
"style/*"                    = "off"
"expression/unknown-context" = "warn"
"uses/unpinned-ref"          = { level = "error", fix = "unsafe", options = { allow = ["actions/*"] } }
"preview/*"                  = "info"

[[overrides]]
include = [".github/workflows/release-*.yml"]  # globset, "!" negates
rules   = { "permissions/missing" = "off" }
```

Resolution, most specific wins:

1. `[[overrides]]` whose `include` matches the document, last match wins
   (biome), then
2. exact id, then area glob `area/*`, then category glob `@category`
   (`"@pedantic" = "warn"`), then
3. `preset`.

CLI `--rule <id>=<level>` and `--preset` sit above all of that. There is
no `select`/`ignore`/`extend-select` triplet (ruff) — one map with globs
is enough and has no precedence puzzles between three keys.

Levels: `off | on | info | warn | error`. `info` never affects exit code.
`warn` does only with `--fail-on warn`. `error` always (ADR-0011,
default `--fail-on error`).

`fix` per rule: `"none" | "safe" | "unsafe"` reclassifies the rule's fix
applicability (ADR-0007) for `--fix` purposes. It does not change the edit.

`options` is a typed table per rule, validated by the rule's own
`schemars`-derived options struct. The global schema embeds them under
`rules.<id>.options`. Unknown option → diagnostic at the config
`SourceRef`.

Unknown rule id in config → `config/unknown-rule` diagnostic, not a fatal
error. Removed rule id → `config/removed-rule` with the replacement if
any.

Extensions: their rules appear under the same `[rules]` table with their
namespace (`"shellcheck/*" = "warn"`). The extension's manifest declares
category and default level per rule. Config overrides them identically.

## Inline suppression

YAML comments are CST nodes (ADR-0002), so suppressions are positional
and survive edits.

```yaml
- run: echo ${{ github.event.issue.title }} # actionlint-ignore expression/untrusted-input: title is rendered, not executed
# actionlint-ignore uses/unpinned-ref, uses/unresolved: internal registry
- uses: internal/thing@main
```

```yaml
# actionlint-ignore-file preview/parallel-step: org has the feature flag
```

```yaml
# actionlint-ignore-start script/deprecated-command: legacy block, tracked in #123
…
# actionlint-ignore-end script/deprecated-command
```

- Trailing comment → the node on that line. Own-line comment → the next
  node. `-file` → whole document, must appear before the first key.
  `-start`/`-end` → range, ids must match.
- **Reason is mandatory** (biome). Missing → `suppression/missing-reason`
  (`error`) and the suppression is still honoured, so a broken comment
  never silently un-suppresses.
- Unused suppression → `suppression/unused` (`warn`), with a fix that
  deletes the comment (biome `suppressions/unused`, clippy
  `unfulfilled_lint_expectations`).
- Unknown id → `suppression/unknown-rule` (`warn`).
- Blanket `# actionlint-ignore: reason` with no ids is **not** supported.
  Clippy has no blanket, biome has `lint` as a blanket and it is the first
  thing people abuse. If you need everything off, that is a config
  override.
- Ids may be globbed the same way as config: `# actionlint-ignore
  preview/*: …`.

## Stability

- New rules start in `nursery`, off, and may change id, message, or
  behaviour without a major bump. They are listed in the docs with a
  marker.
- Promotion to a stable category sets `since` and freezes the id. From
  then on: changing category or default level is a minor bump. Removing
  or renaming is a major bump, with the old id kept in a `renamed`/
  `removed` table so configs and suppressions get a precise diagnostic
  (clippy `deprecated_lints.rs`, ruff `RuleStatus`).
- No `--preview` flag (ruff). A flag is a second config surface. The
  config file is enough.

## What this is not

- No "linter prefix" ids (ruff `F401`): prefixes encode upstream
  provenance we don't have.
- No severity × confidence × persona matrix (zizmor): that is a security
  auditor's model, and security heuristics are extensions here.
- No per-rule Rust `const` severity baked in (clippy): the user can set
  any rule to any level. The category only supplies the default.

## Open

- `@category` glob syntax in config: `@pedantic` vs `category:pedantic`.
  Chosen `@` for brevity. Revisit if TOML quoting makes it ugly.
- Occurrence-targeted suppression (suppress one call path of a reused
  step, not all): possible via `OccurrenceId` (ADR-0006), syntax not
  designed. Not in 2.0.
- Whether `style` default should be `info` rather than `warn`. Biome and
  clippy both say `warn`. Keeping `warn`.
