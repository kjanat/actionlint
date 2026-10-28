# 0017 — runny and commitish are the empirical oracles, their corpora are conformance fixtures

Status: accepted · 2026-10-28 · read from `~/Projects/runny-mc-fuzzface` (`DISCOVERIES.md`, `PATHOLOGICAL.md`, `SCOPE.md`, `corpus/regressions.jsonl`, `repro/`) and `~/Projects/commitish` (`docs/RESEARCH-FINDINGS.md`, `REF-SPELLING.md`, `REVISION-SYNTAX.md`, `NESTING.md`, `RUST-MODEL.md`, `crates/commitish`)

## Context

ADR-0014 names GitHub's *code* as the reference. Two sibling projects have
already measured that code — and github.com itself — far beyond what
reading source gives:

- **runny** (`runny-mc-fuzzface`): a C# oracle linked against the pinned
  `actions/runner` SDK at `80bb1fb8`. Differential-fuzzes the legacy
  (`DTExpressions2`, **what GitHub runs in production**) and next
  (`Sdk/Expressions`, adds `StrictJsonParsing`, off by default) engines,
  the service YAML reader, the step/strategy converters, both action
  manifest loaders, the VS Code `@actions/expressions` evaluator, and
  `kjanat/actionlint` v1.17. 104 machine-readable regressions, 13 runnable
  hosted repros, and a table of where v1 and the service parser disagree.
- **commitish**: controlled github.com experiments on what `uses:
  owner/repo@ref` selects, per consumer (action vs reusable workflow),
  across dispatch/rerun modes, plus a `no_std` Rust model
  (`resolve`, `pin`, `check_pin`, `plan_binding`) with `Profile::{GithubAction,
  GithubWorkflow}`.

Neither is reproducible from docs. Both are reproducible from their repos.

## Decision

1. **Vendor as conformance fixtures.** `xtask vendor-oracles` copies
   `runny/corpus/regressions.jsonl`, `runny/repro/*.yml`, and commitish's
   `corpus/*.json` + `cases/` into `fixtures/conformance/{runny,commitish}/`
   with the source sha. A `conformance` test family replays them:
   every regression's `input` must produce the diagnostic class its
   `family` implies (or none, where `kinds` is empty), and every repro's
   documented runtime verdict must match ours.
2. **`commitish` is a dependency of `semantics`** for `UsesRef`
   classification and of `workspace` for `resolve_uses`/`actionlint fetch`
   (`commitish-github` for REST evidence). No second ref grammar.
3. **Legacy engine semantics, loose JSON, anchors allowed** is the single
   modelled configuration for workflows. Strict JSON and the next engine
   are not modelled. Their differences are reported as
   `expression/engine-divergent` (`pedantic`) from the runny table.
4. **The editor evaluator is not an oracle.** `@actions/expressions`
   diverges from the runtime (`'ß' == 'SS'`, first-vs-last JSON key, `-0`,
   `0X0`, hex `Int32`, depth 50 vs 64). We use languageservices for its
   schema and reader (ADR-0014), never its evaluator.

## Facts absorbed, and what each becomes

### Parsing and `if:` compilation (runny `DISCOVERIES.md` "GitHub Actions semantics")

| fact                                                                                                                                                                                                                  | becomes                                                                                                                                                                                                                                            |
| --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `if:` null, `~`, `""`, whitespace, `${{ '' }}` → `success()`, step **runs**                                                                                                                                           | model: `if` compiles to `success() && (…)` unless it names a status function; blank → `success()`. Rule `step/blank-condition` (suspicious: "always runs, probably not what you meant"). v1's "constant condition / lexer error" verdict was wrong |
| a single string-literal expression `${{ 'false' }}` is folded to text and **re-parsed as condition source**; `${{ 'failure()' }}` becomes `failure()`; `${{ 'nope' }}` rejects the whole workflow                     | model it exactly. `expression/literal-condition-reparsed` (suspicious)                                                                                                                                                                             |
| YAML `True`/`FALSE`/`Null` normalise and compile; quoted `"True"` rejects                                                                                                                                             | `expression/unknown-context` must not fire on bare YAML booleans. v1 did                                                                                                                                                                           |
| `${{ false }}a` in `if:` → `format('{0}a', false)`, always true                                                                                                                                                       | `expression/condition-always-true` (correctness)                                                                                                                                                                                                   |
| `timeout-minutes: '5'`, `continue-on-error: yes`, `${{ 'True' }}` rejected at parse; `1.5` → `1`; `0x10` → 16; `max-parallel: .5` accepted                                                                            | `schema/type-mismatch` follows the converter, not "integer"                                                                                                                                                                                        |
| job with `runs-on` and no `steps`; empty `on:`; empty `run:` — all accepted by the service                                                                                                                            | not errors. `job/empty-steps` is `style`                                                                                                                                                                                                           |
| a second `---` document rejects; a leading BOM makes the service read `﻿on` and reject                                                                                                                                 | `syntax/multiple-documents`, `syntax/bom` (correctness, workflow only — manifest loaders strip BOM)                                                                                                                                                |
| `!!str` is the service spelling; `!!string` legacy-only; tags on mappings/sequences never inspected; `!!bool yes` fails; explicit tags don't override the field's schema type; `if: !fromJSON(x)` is a YAML tag error | `syntax/unknown-tag`, `syntax/tag-on-collection-ignored` (pedantic), `expression/bang-is-yaml-tag` (correctness, with fix: quote it)                                                                                                               |
| anchors: service reader takes `allowAnchors`; hosted accepts them (this repo's CI proves it); **both action-manifest loaders reject every anchor** (`Anchors are not currently supported`)                            | `action/anchor-not-supported` (correctness; this is kjanat/actionlint#338's loader half)                                                                                                                                                           |
| composite step may not carry `timeout-minutes`; `input-default-context` lacks `inputs`; `output-value` lacks `secrets`                                                                                                | all data-driven from the vendored action schema + runner loader                                                                                                                                                                                    |
| U+FEFF/U+200B/U+00AD/U+200E before `./x` in `uses:` pass the culture-sensitive `StartsWith("./")` and load as a local path with the invisible char kept                                                               | `uses/invisible-prefix` (correctness)                                                                                                                                                                                                              |
| `010` is octal 8 in the public SDK but decimal 10 on github.com (hosted run 36666843138)                                                                                                                              | `expression/leading-zero-number` (suspicious): local and hosted disagree, don't rely on either                                                                                                                                                     |

### Expression evaluation (runny, both engines agree unless noted)

| fact                                                                                                                                                                                                                      | becomes                                                                                                                                                                                                                                                                           |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| ``&&``/`                                                                                                                                                                                                                  |                                                                                                                                                                                                                                                                                   | ``return operands;``case``keeps falsy results and short-circuits unused branches; predicates must be``Boolean` or throw | ADR-0016 as written |
| `fromJSON` objects are **case-insensitive dictionaries**: exact duplicates collapse first (last wins, position kept), then case-fold assigns last value under first spelling. `{"a":1,"A":2,"a":3}.a` = 2 loose, 1 strict | type `map<T>` keys are case-insensitive. `expression/json-key-collision` (correctness) when a constant JSON has keys equal under `OrdinalIgnoreCase`; preserve every occurrence in the model so both exact and case collisions are reported together                              |
| `steps.*` and `github.*` contexts are case-insensitive too (`StepsContext`, `GitHubContext`)                                                                                                                              | `step/output-name-collision` (suspicious) across a job's step outputs                                                                                                                                                                                                             |
| string `==`/`startsWith`/`contains` are `OrdinalIgnoreCase`, incl. `'É' == 'é'`, excl. `'ß' == 'SS'`                                                                                                                      | `expression/case-insensitive-gate` (suspicious) when a comparison's other side is an attacker-namable context (`github.head_ref`, `github.ref`, `github.event.*.name`, deployment environment). Measured in the wild: TryGhost/Themes `!startsWith(github.head_ref, 'renovate/')` |
| truthiness and equality disagree: `'0'` truthy yet `'0' == false`; `'' == 0`, `0 == '0'`, `'' != '0'`; comparisons pick string-or-number per pair so `<` is not transitive                                                | `expression/string-truthiness`, `expression/cross-type-compare` (suspicious: declared string input vs numeric literal; `"0x80000000"` reads as negative `Int32`), `expression/chained-compare-mixed-kinds` (pedantic)                                                             |
| `toJSON(-0)` differs between engines; `fromJSON(toJSON(x)) ≠ x` for `-0`, `NaN`, `Infinity`, duplicate keys; lone surrogate → U+FFFD silently                                                                             | `expression/engine-divergent` (pedantic). Not modelled otherwise                                                                                                                                                                                                                  |
| `fromJSON` loose = Json.NET: comments, trailing data, `[1,,2]` → `[1,"",2]`, `new Date()` → string, `undefined` → null, `{a:1}`, `0xFF`, `.5`; `new (` throws a raw `ArgumentException`                                   | `expression/fromjson-lenient` (style) enumerates exactly these; `expression/fromjson-crash` (correctness) for the `new (` shape                                                                                                                                                   |

### Templates (runny)

| fact                                                                                                                                                                                                  | becomes                                                                                                                         |
| ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `${{ insert }}` works in `env`, `with`, matrix axes; conditional insertion via `case`/`&&`; computed keys `${{ format(x) }}: v`                                                                       | model as `MappingInsert` node; `type_env` merges the inserted mapping's type. v1 reported `undefined variable "insert"` — wrong |
| an expression returning an array at a **sequence-item** position splices one level; literal arrays stay nested; `[]` removes the entry                                                                | model `SequenceSplice`; `matrix/expression-splice` (info): job count depends on the expression's arity                          |
| matrix `include`/`exclude` leaves become `matrix['k'] == 'v'` expressions evaluated with loose `==`: case variants match, `"0"` matches `false`, missing nested property is `null == false` → matches | `matrix/filter-loose-equality` (suspicious) when a filter value's type admits coercion collisions; v1 silent                    |
| Azure template features (`each`, structural `if`, `split`/`replace`/`trim`/`lower`/`upper`, step/job generation) are **rejected**                                                                     | `expression/unknown-function` stays an error for those names, with a message saying they are Azure Pipelines, not Actions       |
| composite main steps: `success()`/`failure()` read *action* status, `cancelled()` reads *job* status                                                                                                  | `type_env` carries the enclosing `(action, stage)`; `step/status-function-in-composite` (pedantic)                              |

### References and binding (commitish)

| fact                                                                                                                                                                                                                                                                                                                                                                     | becomes                                                                                                                                                                                            |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| ref spelling is consumer-specific: `tags/x` = tag for an action, literal branch for a reusable workflow; `refs/heads/…`, `HEAD`, `^`, `~1`, `^{}`, `^{commit}`, uppercase SHA, annotated-tag-object SHA work for actions and are rejected for reusable workflows; `@` in a branch works for workflows, rejected for actions; 4/12/39-char abbreviations rejected by both | `UsesRef` classification uses `commitish::RefForm::classify` + `Profile`. `uses/ref-rejected-by-consumer` (correctness) per profile. Never normalise, case-fold, or NFC a ref — bytes are identity |
| `action.yml` beats `action.yaml`; malformed `action.yml` does **not** fall back                                                                                                                                                                                                                                                                                          | `resolve_uses` mirrors; `action/yaml-shadowed` (suspicious) when both exist                                                                                                                        |
| in a remote composite, `./child` resolves from the **runner workspace** (caller's checkout, or nothing), `$/child` from the declaring source repo                                                                                                                                                                                                                        | `uses/dot-slash-in-remote-composite` (suspicious): points at whatever was checked out, not at the action's own files                                                                               |
| nested local reusable refs belong to the callee's repo+commit                                                                                                                                                                                                                                                                                                            | `invocation_graph` resolves `./.github/workflows/x.yml` inside a called workflow against the callee, not the root                                                                                  |
| binding time differs: an action binds at job start, a reusable workflow at dispatch; "rerun one job"/"rerun failed" keep workflow A and re-resolve action B → **mixed-generation execution**                                                                                                                                                                             | rationale text for `uses/unpinned-ref`; `call/mixed-generation` (pedantic) when a reusable workflow is pinned but an action inside it is not                                                       |
| a missing remote action in a *skipped* step still fails preparation; a missing action in a skipped *job* does not; a missing reusable workflow behind a skipped job rejects at dispatch                                                                                                                                                                                  | `uses/unresolved` severity: step-level `correctness` even under `if: false`; job-level reusable `correctness`; job-level action `suspicious`                                                       |
| cache hit ≠ content identity; completion watermark can execute stale content on self-hosted                                                                                                                                                                                                                                                                              | out of scope for a linter; documented, not ruled                                                                                                                                                   |

### Where v1 was wrong against the service (runny "What actionlint and the service parser disagree on")

`run: ! y` (service rejects, v1 accepts), second `---` (same), `if: ' '`
(v1 false positive), job without `steps` (v1 stricter), empty `on:`/`run:`
(v1 stricter), `max-parallel: .5` (v1 stricter), `timeout-minutes: ${{
false }}` (parse-time accept, v1 error), `runner-label` and `shell-name`
classes. Each is a conformance fixture with the service verdict as truth.

## Consequences

- `fixtures/conformance/` is the first test suite to pass, before any
  hand-written rule fixtures. A rule that contradicts a runny verdict is
  wrong by definition unless the hosted run disagrees with the SDK (the
  `010` case), in which case hosted wins and the fixture says so.
- `commitish` enters the workspace as a path/git dependency pinned by
  sha. Its `Profile` enum is the consumer axis of `UsesRef`.
- ADR-0006, ADR-0016 and `design/rules.md` are amended to point here
  rather than restating the tables.
- Re-vendoring is tied to the runner sha runny pins. When runny moves its
  pin, the conformance fixtures move with it, and any newly failing
  fixture is a real runtime change, not a flake.

## Open

- runny's `wasi-host` exposes the oracle as a WASI component. Whether to
  run it in CI for property tests against our evaluator (rather than only
  replaying the frozen corpus) — attractive, needs the .NET WASI toolchain
  in CI.
- `StrictJsonParsing` on github.com: unknown. If it ever ships, the
  `fromJSON` model flips first-wins/last-wins. Watch runny.
