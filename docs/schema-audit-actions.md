# Action metadata schema audit

Audited 2026-09-13 against all 25 definitions in the runner's
[action_yaml.json at 759385a3510197a58b5c08dc1f373b74b9f4643b](https://github.com/actions/runner/blob/759385a3510197a58b5c08dc1f373b74b9f4643b/src/Runner.Worker/action_yaml.json),
[ActionManifestManager](https://github.com/actions/runner/blob/759385a3510197a58b5c08dc1f373b74b9f4643b/src/Runner.Worker/ActionManifestManager.cs),
[TemplateReader](https://github.com/actions/runner/blob/759385a3510197a58b5c08dc1f373b74b9f4643b/src/Sdk/WorkflowParser/ObjectTemplating/TemplateReader.cs),
and the current [public metadata reference](https://docs.github.com/en/actions/reference/workflows-and-actions/metadata-syntax).
The schema file's last modification is `fee24199cba8acf6c25da3a061c986066f42327a`;
the generated tables retain that precise source revision. The fixture contains
the complete schema from the audited runner revision.

## Coverage inventory

Every definition is represented in the generated structural table. The table
and expression availability table now come from the same generator invocation.
`checkActionMetadataSchema` traverses raw YAML retained alongside the metadata
used for checking workflow callers. Existing precise runtime and composite-step
diagnostics handle execution requirements.

| Definition               | Fields or type                                                                                          | Handling                                                                                                                                  |
| ------------------------ | ------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `action-root`            | `name`, `description`, `inputs`, `runs`, `outputs`; additional nonempty keys with arbitrary values      | Root shape and known fields checked; unknown extensions remain allowed, matching runner. Existing name/description checks retained.       |
| `inputs`                 | Mapping of nonempty names to `input`                                                                    | Shape, names and duplicate input IDs checked.                                                                                             |
| `input`                  | `default`; additional nonempty keys with arbitrary values                                               | Shape and default schema checked; existing public `required`, `description`, `deprecationMessage` metadata behavior retained.             |
| `input-default-context`  | String; github, strategy, matrix, job, runner; hashFiles(1-255)                                         | Context, function arity and expression syntax checked.                                                                                    |
| `outputs`                | Mapping of nonempty names to `output-definition`                                                        | Shape, names and duplicate output IDs checked.                                                                                            |
| `output-definition`      | `description`, `value`; closed mapping                                                                  | Unknown fields, nested value shape and expression restrictions checked.                                                                   |
| `output-value`           | String; github, strategy, matrix, steps, inputs, job, runner, env                                       | Availability and syntax checked. Status functions/hashFiles remain unavailable here.                                                      |
| `runs`                   | Union of container, node, plugin, composite                                                             | Runtime selects descriptor; unsupported runtime and cross-runtime fields diagnosed.                                                       |
| `container-runs`         | `using`, `image`, `entrypoint`, `args`, `env`, `pre-entrypoint`, `pre-if`, `post-entrypoint`, `post-if` | All fields checked. Hook conditions allowed. Entrypoints resolve inside image; no local-file requirement. Local Dockerfile still checked. |
| `container-runs-args`    | Sequence of `container-runs-context`                                                                    | Sequence and every argument checked.                                                                                                      |
| `container-runs-context` | String; inputs context                                                                                  | Container argument syntax/context checked.                                                                                                |
| `container-runs-env`     | Mapping, nonempty keys/string values; inputs context                                                    | Shape, every key/value and expression availability checked.                                                                               |
| `node-runs`              | `using`, `main`, `pre`, `pre-if`, `post`, `post-if`                                                     | All fields checked; main and local script files checked. Existing hook-pair diagnostics retained.                                         |
| `plugin-runs`            | Nonempty `plugin`; closed mapping                                                                       | Runner-internal form represented and structurally validated. Plugin installation and execution require the internal runner loader.        |
| `composite-runs`         | `using`, `steps`                                                                                        | Closed shape and required steps checked; empty sequence remains accepted by runner.                                                       |
| `composite-steps`        | Sequence of `composite-step`                                                                            | Shape and every step checked.                                                                                                             |
| `composite-step`         | Union of `run-step`, `uses-step`                                                                        | Mutual exclusion, required execution field and allowed keys checked.                                                                      |
| `run-step`               | `name`, `id`, `if`, required `run`, `env`, `continue-on-error`, `working-directory`, required `shell`   | Every field's shape, expression availability and execution requirements checked.                                                          |
| `uses-step`              | `name`, `id`, `if`, required `uses`, `continue-on-error`, `with`, `env`                                 | Every field's shape and expression constraints checked. Reusable-workflow references remain forbidden.                                    |
| `non-empty-string`       | Scalar coercible to nonempty string                                                                     | Empty/null rejected where required; booleans/numbers converted as runner does.                                                            |
| `string-steps-context`   | String; github, inputs, strategy, matrix, steps, job, runner, env; hashFiles(1-255)                     | Shared composite string fields checked.                                                                                                   |
| `boolean-steps-context`  | Boolean or expression; same contexts/functions                                                          | Invalid literals/collections rejected; expressions deferred to runner for evaluation.                                                     |
| `step-env`               | Mapping of nonempty keys/string values; composite contexts                                              | Keys, values, full-map expressions and insertion mappings checked.                                                                        |
| `step-if`                | String condition; composite contexts plus status functions/hashFiles                                    | Bare and delimited expression syntax, available contexts/functions and arity checked.                                                     |
| `step-with`              | Mapping of nonempty keys/string values; composite contexts                                              | Keys, values, full-map expressions and insertion mappings checked.                                                                        |

## Fixed failures

The initial 33-case regression run failed 28 cases before changes. Those failures
covered valid Docker hook conditions rejected; output declarations ignored;
Docker args/env shapes and contexts ignored; unknown/empty runs properties
ignored; composite optional-field shapes and booleans ignored; valid scalar
script values rejected; malformed expressions/unknown functions ignored; and
empty input IDs/null input declarations accepted.

Further runner conversion checks fixed container entrypoint paths incorrectly
required in the source directory and composite IDs that were unchecked. The
runner's [IdBuilder](https://github.com/actions/runner/blob/759385a3510197a58b5c08dc1f373b74b9f4643b/src/Sdk/WorkflowParser/Conversion/IdBuilder.cs)
rejects duplicate IDs case-insensitively, reserved `__` prefixes, invalid ASCII
identifier characters and lengths of 100 or more. Boundary tests cover these.
Literal string expressions remain valid escapes; escaped expression markers
inside scripts are not evaluated twice. Mapping-key expressions receive the
same context checks as mapping values.
Recursive YAML aliases used in insertion mappings are bounded by the runner's
100-level template depth limit and covered by a regression.
The complete physical metadata YAML tree also shares the workflow parser's
explicit-tag validation. Legacy `!!bool on`, binary integer tags, quoted
non-string tags and custom scalar tags cannot bypass runner YAML grammar by
appearing in a string field or an otherwise uninterpreted extension.

Action-expression checks now reject malformed/unterminated expressions,
unknown functions, invalid builtin arities, invalid `case` argument counts and
the runner's 255-argument caps. They intentionally do not invent types for
caller-owned input/context properties.

Workflow calls supplying an entire `with` expression no longer receive false
missing-required-input diagnostics: their keys are determined at evaluation.
The workflow expression rule checks the expression's known shape separately.

## Compatibility choices

- Runner scalar conversion is authoritative for schema strings, including
  booleans/numbers in `run`/`shell` and boolean Docker hook conditions. Public
  documentation's use of the word "string" does not require YAML quoting.
- Docker script entrypoints refer to files/commands inside the image. Only a
  local Dockerfile can be verified from repository contents. Image execution,
  plugin assembly loading and runtime expression values require the runner.
- The public JavaScript runtimes are node12/node16/node20/node24 in the pinned
  runner loader. Existing lifecycle diagnostics distinguish removed/deprecated
  runtimes from unrecognized metadata values; public docs currently use node24.
- The runner schema deliberately leaves root extensions and most input
  documentation metadata open. Existing stricter checks for required action
  name/description, supported input metadata keys, required inputs, branding,
  and deprecated-input messages are retained. This audit adds no blanket
  "all documentation fields required" rule where the runtime schema is open.
  Input/output descriptions therefore remain optional to the runner validator.
- `author` and other root extensions are accepted without being execution
  inputs. Branding icon/color checks remain the existing public-doc checks.
- Internal `plugin-runs` is structurally supported without presenting `plugin`
  as a public `runs.using` value. It does not require a fictitious `using` key.
- Full-map expressions are accepted in composite `env`/`with`. The container
  loader's `runs.env`/`runs.args` still requires an actual mapping/sequence;
  expression values inside those collections are validated in their own context.
- Missing local actions are still tolerated because workflows may create or
  check out them later. Files in referenced local actions are checked through
  the existing local-action cache; remote repositories are not fetched by this
  validator.

## Verification and maintenance

`TestSchemaActionDefinitionCoverage` locks the complete 25-definition inventory
and executable union variants. `TestSchemaActionEveryProperty` exercises invalid
shapes for every explicit property across all nine property-bearing mappings.
`TestSchemaActionAudit` covers valid/invalid behavior, boundaries, contexts,
escaping, plugin shape and Docker behavior. Existing action metadata/cache,
expression and action-project tests pass.

`TestCompleteRunnerActionSchema` regenerates the complete vendored schema and
compares it with the checked-in structural/context tables. Generator tests also
cover deterministic output, invalid references, cycles and lost required
expression fields. Unsupported new descriptor structures fail generation.

Passed focused commands:

```sh
go test . -run '^(TestAction|TestComposite|TestLocalActions|TestSchemaAction)' -count=1
go test ./scripts/generate-action-metadata -count=1
go test . -run '^TestLinterLintProject/project/(local_docker_action|local_composite_action|composite_action_steps|composite_action_context|action_metadata_expressions|local_action_invalid|local_action_invalid_runners|local_javascript_action|local_action_branding|local_action_yaml_1.1_boolean|local_action_case_insensitive|yaml_anchor_in_action|broken_local_action|local_action_empty)$' -count=1
```

These checks ran locally in the isolated audit checkout. No remote CI or
GitHub runner execution was performed. Full-repository integration and
upstream conformance results are recorded by the combined schema audit.
