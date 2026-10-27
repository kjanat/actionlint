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

| path                                         | what it is                                                                                                                                                                                                                             |
| -------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `src/Sdk/WorkflowParser/workflow-v1.0.json`  | runner's copy of the workflow schema. ≥306 definitions. Has `snapshot`, `image-version`, `parallel-step`/`wait-step` **not found** in either read portion                                                                              |
| `src/Sdk/DTPipelines/workflow-v1.0.json`     | legacy copy, 47 definitions. Ignored                                                                                                                                                                                                   |
| `src/Sdk/WorkflowParser/ObjectTemplating/*`  | C# template reader/evaluator, incl. `Tokens/InsertExpressionToken.cs`, `TemplateUnraveler.cs` (expression expansion), `TemplateEvaluator.cs`                                                                                           |
| `src/Sdk/DTExpressions2/*`                   | C# expression parser, `Sdk/Functions/{Case,Contains,EndsWith,Format,FromJson,Join,NoOperation,StartsWith,ToJson}.cs`                                                                                                                   |
| `src/Runner.Worker/action_yaml.json`         | runner's action schema. `runs.using` is only `non-empty-string` here                                                                                                                                                                   |
| `src/Runner.Worker/ActionManifestManager.cs` | where `using` is really decided: `docker`, `node12`, `node16`, `node20`, `node24`, `composite`, compared `OrdinalIgnoreCase`, `pre-if`/`post-if` default `always()`, `image`/`main`/`steps` requiredness and their exact error strings |

### 3. docs.github.com

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
  - only in runner (≥306): `workflow-root-new`, `on-new`, `on-string`,
    `sequence-of-on-string`, `on-mapping-new`, the `*-relaxed` family
    (`schedule-relaxed`, `workflow-dispatch-*-relaxed`,
    `on-event-config-*-relaxed`, `event-property-strings-relaxed`),
    `dynamic-string`, `interactive-component-string`, `member-string`,
    `snapshot-image-name`
    Both are vendored. The union is the accepted surface. The intersection
    is the documented-stable surface. The symmetric difference is exactly
    what `preview/*` rules flag. `xtask vendor-github` prints this diff on
    every update so a change in either copy is noticed.
- Licences: `actions/runner` MIT, `actions/languageservices` MIT
  ("Copyright GitHub"). Vendoring their JSON data is permitted with
  attribution. `semantics/vendor/LICENSE-github` carries it.

## Open

- The runner schema's tail (chars 100000–101439) was read separately and
  adds only `string-runner-context-no-secrets`, `string-steps-context`,
  `shell`, `working-directory`, `cron-mapping`, `cron-pattern`. Exact
  runner count still unconfirmed. `xtask vendor-github` will report it.
- What the runner does with a `*-relaxed` / `-new` definition at runtime
  (feature flag? GHES?) is unknown. Model them, flag them, don't guess.
