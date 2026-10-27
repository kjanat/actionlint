# 0014 — Source of truth is GitHub's runner schema and template semantics, not docs.github.com

Status: accepted · 2026-10-28 · verified against `actions/languageservices` and `actions/runner` main (fetched 2026-10-27/28)

## Context

docs.github.com describes a subset of what GitHub accepts. Three real,
running workflows (kjanat/actions-rs `ci.yml`, `crates-io.yml`,
kjanat/shellcheck-rs `wiki.yml`) use, and GitHub executes, all of:

- `matrix: |-` — a block scalar whose entire content is one expression
  that evaluates to the matrix mapping
- function names in arbitrary case: `FrOmJsOn(tOjSoN(fRoMjSoN(…)))`
- `fromJSON` input with comments, trailing commas, unquoted keys, octal
  `010`, `''` doubled quotes, and trailing prose after the closing brace
- `${{ insert }}:` as a mapping key under `env:`
- `with: ${{ fromJSON(vars.REPO) }}` — a whole mapping from an expression
- `steps: [uses: a, uses: b]` — flow sequence of single-pair mappings
- YAML anchors/aliases on `runs-on:`, on whole steps, on `uses:` values
- `if: ${{ format('{0}', false) }}` — string `"false"` coerced to false
- `steps: [{ parallel: [*GIT, *RUN, *RST] }]`
- `shell: "actions-shell cargo-script {0}"` — custom shell template
- `workflow_dispatch: null`, `permissions: {}`

None of that is specified on docs.github.com. All of it is specified, as
code, in GitHub's own parser.

## Decision

"GitHub's reference" means these artefacts, in this precedence:

### 1. `actions/languageservices` (TypeScript, GitHub's own parser, shipped in the VS Code extension)

| path                                                                                      | what it is                                                                                                                                                                                                                           |
| ----------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `workflow-parser/src/workflow-v1.0.json`                                                  | the workflow template schema. 290 definitions. Includes `parallel-step`, `parallel-steps`, `wait-step`, `wait-all-step`, `cancel-step`, `step-background`, `snapshot`, `image-version` — feature-gated at GitHub, undocumented, real |
| `workflow-parser/src/action-v1.0.json`                                                    | the `action.yml` schema, 25 definitions                                                                                                                                                                                              |
| `workflow-parser/src/templates/template-reader.ts`                                        | the template reader: how YAML becomes typed tokens under the schema                                                                                                                                                                  |
| `workflow-parser/src/templates/schema/*.ts`                                               | definition types: `mapping`, `sequence`, `one-of`, `string`, `number`, `boolean`, `null`, `allowed-values`, `loose-key-type`/`loose-value-type`, `require-non-empty`, `context`                                                      |
| `workflow-parser/src/templates/tokens/*.ts`                                               | token model incl. `insert-expression-token.ts`                                                                                                                                                                                       |
| `expressions/src/{lexer,parser,funcs,errors}.ts`, `funcs/*.ts`                            | the expression language                                                                                                                                                                                                              |
| `languageservice/src/context-providers/*.ts`, `events/webhooks.json`, `descriptions.json` | context shapes (`github`, `env`, `job`, `steps`, `needs`, `matrix`, `inputs`, `secrets`), event → activity-type table (kept in sync with the schema by `schema-sync.test.ts`), property descriptions                                 |

### 2. `actions/runner` (C#, what actually executes)

Local checkout: `~/Projects/runny-mc-fuzzface/third_party/actions-runner`
at `80bb1fb8` (2026-09-14). `xtask vendor-github` reads from there when
present, network otherwise.

**The runner does not parse workflow files.** `JobExtension.InitializeJob`
receives `Pipelines.AgentJobRequestMessage` with `message.Steps` already
expanded by the GitHub service, including `Action.Background` and
`Action.ParallelGroupId` (`JobExtension.cs` ~470). Its
`GitHub.Actions.WorkflowParser` namespace is consumed only by
`ActionManifestManager` (action.yml), the four `Runner.Worker/Expressions`
functions, and `PipelineTemplateEvaluatorWrapper`. So the runner's
workflow schema copy is **not** what validates user YAML on github.com —
that parser is server-side and closed. `actions/languageservices` is the
only public artefact of it, which is why it, not the runner, knows
`parallel-step`, `wait-step`, `cancel-step`, `step-background`,
`concurrency-queue`. The runner *executes* those
(`BackgroundStepCoordinator.cs`, `ExecutionContext.IsBackground`,
`StepsRunner.cs`) without ever seeing their YAML. `concurrency.queue` has
zero footprint in the runner: pure service-side.

Precedence therefore: workflow syntax → languageservices schema; action
syntax → runner `ActionManifestManager` + `action_yaml.json`; expression
functions and `fromJSON` leniency → runner `DTExpressions2`; execution
semantics (background/parallel/wait/cancel ordering, `pre-if`/`post-if`)
→ runner `Runner.Worker`.

| path                                         | what it is                                                                                                                                                                                                                                                                                                                                                                     |
| -------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `src/Sdk/WorkflowParser/workflow-v1.0.json`  | runner's copy of the workflow schema. **312** definitions at `80bb1fb8`. Has `snapshot`, `image-version`, `workflow-root-new`, `on-new`, `*-relaxed`, `dynamic-string`. No `parallel-step`/`wait-step`/`cancel-step`/`step-background`/`concurrency-queue` (grep-confirmed on the local checkout). Used by nothing in the runner for workflows — vendored for cross-check only |
| `src/Sdk/DTPipelines/workflow-v1.0.json`     | legacy copy, 47 definitions. Ignored                                                                                                                                                                                                                                                                                                                                           |
| `src/Sdk/WorkflowParser/ObjectTemplating/*`  | C# template reader/evaluator, incl. `Tokens/InsertExpressionToken.cs`, `TemplateUnraveler.cs` (expression expansion), `TemplateEvaluator.cs`                                                                                                                                                                                                                                   |
| `src/Sdk/DTExpressions2/*`                   | C# expression parser, `Sdk/Functions/{Case,Contains,EndsWith,Format,FromJson,Join,NoOperation,StartsWith,ToJson}.cs`                                                                                                                                                                                                                                                           |
| `src/Runner.Worker/action_yaml.json`         | runner's action schema. `runs.using` is only `non-empty-string` here                                                                                                                                                                                                                                                                                                           |
| `src/Runner.Worker/ActionManifestManager.cs` | where `using` is really decided: `docker`, `node12`, `node16`, `node20`, `node24`, `composite`, compared `OrdinalIgnoreCase`, `pre-if`/`post-if` default `always()`, `image`/`main`/`steps` requiredness and their exact error strings                                                                                                                                         |

### 3. `actions/runner-images` (hosted runner labels)

`README.md` "Available Images" table: image, architecture, YAML labels,
and a per-image gist JSON badge. The README is the only place the label
set lives (19 images, 29 labels on 2026-10-28, incl. `ubuntu-slim`,
`ubuntu-26.04-arm`, `windows-2025-vs2026`, `windows-11-vs2026-arm`,
`macos-26-intel`, deprecated `macos-14*`, preview `xcode-27*`).
`xtask vendor-github` parses that table into
`semantics/vendor/runner-labels.json`; `runner/unknown-label` and
`runner/deprecated-image` read it. Self-hosted labels come from config.

### 4. `webhooks.json` payload trees

`languageservice/src/context-providers/events/webhooks.json` (756 KB)
keys events → activity types → `bodyParameters` with nested
`childParamsGroups`: the payload property tree, by name, with
descriptions. That is the source for typing `github.event.*` per
`on:` event, so `github.event.pull_request.<unknown>` can be reported
(`expression/unknown-event-property`, `suspicious`). Types are not
listed, only names.

### 5. docs.github.com

Explanatory. Used for diagnostic help links, rule rationale text, and
nothing normative.

## Facts the design relies on (all read from source)

Template reader (`template-reader.ts`):

- `${{ insert }}` is matched case-sensitively, must be the entire scalar,
  and as a key it is accepted only where the definition's `context` is
  non-empty. It is recorded as an expression-keyed entry. Expansion
  happens later.
- An expression as a mapping key sets `hasExpressionKey`, which disables
  the required-property check for that mapping.
- An expression as a value (`with: ${{ … }}`) is one `BasicExpressionToken`.
  It is never parsed as a mapping at read time. Allowed iff
  `definition.allowedContext` is non-empty.
- `a ${{ x }} b` becomes `format('a {0} b', x)`.
- Duplicate keys are detected case-insensitively (`toUpperCase`).
- Messages: `Unexpected value '…'`, `Required property is missing: …`,
  `'…' is already defined`, `A sequence was not expected`, `A mapping was
  not expected`, `A template expression is not allowed in this context`,
  `There's not enough info to determine what you meant. Add one of these
  properties: …`.
- `context` arrays list contexts and functions with arities, e.g.
  `hashFiles(1,255)`, `always(0,0)`. That array is the machine-readable
  "which context is available where" table.

Expressions (`expressions/src`):

- Function names and context names are lowercased before lookup. Eight
  well-known functions: `case`, `contains`, `endswith`, `format`,
  `fromjson`, `join`, `startswith`, `tojson`. `always`/`success`/
  `failure`/`cancelled`/`hashFiles` arrive via `context` arrays as
  extension functions.
- `MAX_PARSER_DEPTH = 50`, `MAX_EXPRESSION_LENGTH = 21000`.
- Precedence (from parser comments): `!` > comparison > `==`/`!=` >
  `&&` > `||`.
- Error set: unexpected symbol, unrecognized named-value, unexpected end,
  exceeded max depth/length, too few/many parameters, even parameters
  (`case`), unrecognized context, unrecognized function.

Actions (`ActionManifestManager.cs`): the `using` set and all required
field errors quoted above. Schema alone does not constrain `using`.

## Consequences

- `semantics` vendors, at a pinned commit, these data files:
  `workflow-v1.0.json`, `action-v1.0.json`, `webhooks.json`,
  `descriptions.json`, plus the runner's `workflow-v1.0.json` for
  cross-check. `xtask vendor-github <sha>` updates them. The sha is
  recorded in `semantics/vendor/GITHUB.lock`. This is data, in the same
  class as fixtures, not generated code (ADR-0012).
- The first rule family is **schema-driven** and reads those files:
  unexpected key, missing required, wrong type, `one-of` ambiguity,
  expression not allowed here, unknown context here, unknown function,
  arity, depth/length. Messages mirror the runner's wording so a user sees
  locally what GitHub would show.
- Everything the reader accepts, `syntax`/`semantics` accept. Anchors,
  flow collections, expression-valued mappings, `insert`, feature-gated
  step kinds are modelled, not rejected. "GitHub may not have this feature
  enabled for you" is a rule (`preview/*`), not a parse error.
- `fromJSON` leniency is the runner's, not the language service's. The
  runner (`FromJson.cs`) parses with Newtonsoft `JsonTextReader` +
  `JToken.ReadFrom`, `DateParseHandling.None`,
  `FloatParseHandling.Double`, no `MaxDepth`, no comment setting, no
  try/catch, and **no check for trailing content** — `ReadFrom` consumes
  one token and stops. That is why the `ci.yml` matrix with comments,
  trailing commas, unquoted keys, `010` and trailing prose runs on GitHub.
  The language service (`fromjson.ts`) uses `JSON.parse` + a reviver and
  would reject it. Where the two disagree, the runner wins: it is what
  executes. `expression/fromjson-*` rules therefore model Json.NET
  defaults, and a `style/strict-json` rule can flag the leniency.
- Context availability for typing (`type_env`) comes from the `context`
  arrays and `context-providers/*.ts`, never hand-maintained. Entry
  syntax is exactly `/^([a-zA-Z0-9_]+)\(([0-9]+),([0-9]+|MAX)\)$/` for
  functions (`MAX` → `Number.MAX_SAFE_INTEGER`). Anything else is a named
  context. Malformed entries fall through silently
  (`templates/allowed-context.ts`).
- The two schema copies are **neither a subset of the other**:
  - only in languageservices (290): `parallel-step`, `parallel-steps`,
    `wait-step`, `wait-all-step`, `cancel-step`, `step-background`,
    `step-wait-target`, `step-wait-all-value`, `step-cancel-target`,
    `concurrency-queue`, `sequence-of-string`
  - only in runner (312): `workflow-root-new`, `on-new`, `on-string`,
    `sequence-of-on-string`, `on-mapping-new`, the `*-relaxed` family
    (`schedule-relaxed`, `workflow-dispatch-*-relaxed`,
    `on-event-config-*-relaxed`, `event-property-strings-relaxed`),
    `dynamic-string`, `interactive-component-string`, `member-string`,
    `snapshot-image-name`
    Both are vendored. For **workflows** the languageservices copy is
    authoritative (it mirrors the server-side parser; the runner never
    sees workflow YAML). Its extras (`parallel-step` …) are real, executed
    by the runner, feature-gated at the service → `preview/*`. The
    runner-only extras (`*-relaxed`, `-new`, `dynamic-string`) are not
    reachable through github.com's parser as far as public code shows →
    modelled, flagged `preview/*`, never required. `xtask vendor-github`
    prints the diff on every update so a change in either copy is noticed.
- Licences: `actions/runner` MIT, `actions/languageservices` MIT
  ("Copyright GitHub"). Vendoring their JSON data is permitted with
  attribution. `semantics/vendor/LICENSE-github` carries it.

## Open

- Who consumes the runner's `workflow-root-new` / `*-relaxed`
  definitions: no C# reference outside the schema file itself
  (grep on `80bb1fb8`). Possibly GHES or a dead branch. Model, flag,
  don't guess.
- Whether `actions/languageservices` lags or leads the service: the only
  signal is its own release cadence. Pin a sha, re-vendor on a schedule.
