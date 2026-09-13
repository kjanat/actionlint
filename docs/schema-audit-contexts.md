# Workflow expression, context, and scalar audit

Audit date: 2026-09-13. Work performed in the isolated schema-audit checkout.

## Authorities

- [Runner workflow schema, 759385a3510197a58b5c08dc1f373b74b9f4643b](https://github.com/actions/runner/blob/759385a3510197a58b5c08dc1f373b74b9f4643b/src/Sdk/WorkflowParser/workflow-v1.0.json).
- [Languageservices workflow schema, 4043eda158e16579cc5fb1b0b07a4bce2a76f0b5](https://github.com/actions/languageservices/blob/4043eda158e16579cc5fb1b0b07a4bce2a76f0b5/workflow-parser/src/workflow-v1.0.json).
- [Runner workflow converter at the same commit](https://github.com/actions/runner/blob/759385a3510197a58b5c08dc1f373b74b9f4643b/src/Sdk/WorkflowParser/Conversion/WorkflowTemplateConverter.cs): condition contexts, special-function arity, snapshot conversion.
- [Runner TemplateReader](https://github.com/actions/runner/blob/759385a3510197a58b5c08dc1f373b74b9f4643b/src/Sdk/WorkflowParser/ObjectTemplating/TemplateReader.cs): context inheritance, expression rejection in static definitions, sole string-literal folding.
- [Runner YAML reader](https://github.com/actions/runner/blob/759385a3510197a58b5c08dc1f373b74b9f4643b/src/Sdk/WorkflowParser/Conversion/YamlObjectReader.cs): YAML 1.2 core scalar recognition.
- [Runner expression constants](https://github.com/actions/runner/blob/759385a3510197a58b5c08dc1f373b74b9f4643b/src/Sdk/DTExpressions2/Expressions2/ExpressionConstants.cs): builtin function arity.
- [Current context reference](https://docs.github.com/en/actions/reference/workflows-and-actions/contexts), [expression reference](https://docs.github.com/en/actions/reference/workflows-and-actions/expressions), and [workflow syntax](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax), inspected on the audit date.

## Context inventory

Profiles below abbreviate context sets. Contexts inherited from enclosing schema definitions are additive. In particular, credential definitions inherit needs/strategy/matrix from their enclosing container definition.

| Profile | Contexts             | Special functions                              |
| ------- | -------------------- | ---------------------------------------------- |
| W       | github, inputs, vars | none                                           |
| WE      | W + secrets          | none                                           |
| WO      | W + jobs             | none                                           |
| N       | W + needs            | none                                           |
| NI      | N                    | always, cancelled, failure, success            |
| M       | N + strategy, matrix | none                                           |
| ME      | M + secrets          | none                                           |
| D       | M + env              | none                                           |
| C       | ME + env             | none                                           |
| CE      | C + job, runner      | none                                           |
| R       | CE + steps           | none                                           |
| RN      | R minus secrets      | none                                           |
| S       | R                    | hashFiles                                      |
| I       | RN                   | always, cancelled, failure, success, hashFiles |

| Every expression-context definition in the pinned schemas                  | Effective profile / use                                         |
| -------------------------------------------------------------------------- | --------------------------------------------------------------- |
| run-name                                                                   | W                                                               |
| workflow-call-input-default                                                | W                                                               |
| workflow-output-context                                                    | WO                                                              |
| workflow-env                                                               | WE                                                              |
| job-if, job-if-result                                                      | NI                                                              |
| strategy                                                                   | N; inherited by fail-fast, max-parallel, matrix                 |
| snapshot                                                                   | M; runner only; inherited by image-name/version                 |
| snapshot-if                                                                | I in runner; M in languageservices; runner converter confirms I |
| runs-on                                                                    | M; inherited by labels/group                                    |
| job-env                                                                    | ME                                                              |
| workflow-concurrency                                                       | W                                                               |
| job-concurrency                                                            | M                                                               |
| job-environment, job-environment-name                                      | M                                                               |
| job-defaults-run                                                           | D                                                               |
| step-continue-on-error                                                     | S                                                               |
| step-if                                                                    | I                                                               |
| step-if-result                                                             | I minus needs in its own declaration; used during execution     |
| step-env, step-name, step-timeout-minutes, step-with                       | S                                                               |
| container, services, services-container                                    | M                                                               |
| container-registry-credentials                                             | C after enclosing-container inheritance                         |
| boolean-needs-context, number-needs-context, string-needs-context          | N                                                               |
| scalar-needs-context                                                       | M                                                               |
| scalar-needs-context-with-secrets                                          | ME                                                              |
| boolean-strategy-context, number-strategy-context, string-strategy-context | M                                                               |
| boolean-steps-context, number-steps-context, string-steps-context          | S                                                               |
| string-runner-context                                                      | R                                                               |
| string-runner-context-no-secrets                                           | RN                                                              |

The executable audit maps these definitions to all 37 public/generated workflow keys. It checks the exact context/function lists, every builtin global context's acceptance or rejection at each key, and presence in `allWorkflowKeys`. Existing availability tests additionally check reverse context/function indexes. Mapping children use their parent's profile unless a child extends it.

Container/service env retains the published CE profile, although the runner's reusable string-runner-context definition contains steps. Container initialization happens before steps; expanding this public profile based solely on that reused runtime definition would assert unsupported useful step availability.

## Corrected paths

- Snapshot conditions now use I, including runner/env/job/steps, status functions, and hashFiles. They are checked after visiting job steps, so legitimate step outputs resolve and missing step IDs remain errors.
- Snapshot image-name/version now receive expression syntax, type, and M context checks. Previously these expressions were skipped entirely.
- cancel-timeout-minutes receives M availability, matching number-strategy-context; its AST/parser/visitor support is part of the accompanying job audit.
- Generator fallbacks now populate the switch, reverse context map, reverse function map, and key inventory. Previously snapshot.if was absent from the key inventory and had an obsolete context set.
- Background expressions receive boolean/syntax checks. The runner schema omits background; Language Services defines a contextless boolean. The implementation preserves the existing dynamic boolean extension with the established step boolean profile S. This is an explicit compatibility extension beyond the Language Services expression contract.
- Static workflow fields reject computed expressions even if they reference no context. A sole string-literal expression remains valid because TemplateReader folds it; interpolation remains an expression. This closes constant `format(...)` bypasses in shell/name/trigger/static metadata fields.
- Workflow description and image_version types are now visited for expression checks.
- Workflow-dispatch and workflow-call input required flags, and workflow-call secret required flags, are literal booleans. The parser audit rejects even `${{ true }}` there; contextless boolean schema definitions do not allow expressions.
- Language Services defines wait-all as contextless null or boolean. Its ancestors add no expression context, and current workflow syntax says the keyword takes no arguments. The parser now rejects expressions previously accepted and discarded, including computed constant booleans. Literal false retains the existing no-op diagnostic.
- Per-entry matrix include expressions now check the entry itself and require object type; the previous code checked the unrelated whole-include expression. Integration tests live with the nested-schema audit.
- Whole expressions are validated as the schema's evaluated structures: strategy, job defaults.run, container, services, environment, concurrency, snapshot, runs-on, step with, and container/service ports/volumes. Known object keys, required properties, nested types, and scalar conversions are checked; unknown expression types remain accepted. The companion parser changes retain these expressions in explicit AST fields.
- Whole strategy/matrix expressions infer the selected matrix values from axis element types. Previously a statically inferred axis array remained an array in the matrix context, incorrectly rejecting valid string interpolation such as `matrix.os`. Inference no longer deletes include/exclude from the source object type.

These whole-workflow forms were also checked against conversion order and schema contexts. `ConvertToStrategy`, `ConvertToStepInputs`, `ConvertToSnapshot`, `ConvertToConcurrency`, and `ConvertToActionEnvironmentReference` return early for expression tokens during initial validation. Container/services converters defer when their token tree contains expressions. The job retains the original tokens. [`EvaluateJobDefaultsRun`](https://github.com/actions/runner/blob/759385a3510197a58b5c08dc1f373b74b9f4643b/src/Sdk/DTPipelines/Pipelines/ObjectTemplating/PipelineTemplateEvaluator.cs) evaluates before asserting a mapping. This differs from the action-manifest Docker args/env loader, which asserts collections before evaluation; that separate action restriction remains intact.

The evaluated container/credential schemas have no required image/username/password flags. The converter returns optional credentials without a required-field check, and returns no job container when the resulting image is empty. Existing literal-form lint policy is stricter; evaluated object validation does not silently promote that policy into an upstream schema requirement.

## Function inventory

| Function             | Audited contract / result                                                                                                                                                                                                                               |
| -------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| contains             | 2 arguments; string/array overloads retained                                                                                                                                                                                                            |
| startsWith, endsWith | 2 arguments; existing scalar type checks retained                                                                                                                                                                                                       |
| format               | Runner registers 1..255. Added the one-string overload; placeholders still require corresponding arguments. Enforced 255 maximum. Docs currently describe at least one replacement and no maximum, conflicting with the executable parser registration. |
| join                 | 1..2. Added documented string-input overloads; existing array overloads retained.                                                                                                                                                                       |
| toJSON, fromJSON     | 1 argument; existing result typing and literal JSON validation retained                                                                                                                                                                                 |
| case                 | 3..255 and odd argument count. Added maximum; existing odd-count and truthy predicate semantics retained.                                                                                                                                               |
| hashFiles            | 1..255 at allowed step/snapshot positions. Added maximum.                                                                                                                                                                                               |
| always, cancelled    | 0 arguments at allowed condition positions                                                                                                                                                                                                              |
| success, failure     | Job conditions permit job arguments; step/snapshot conditions require 0. Added position-specific signatures without mutating shared builtin tables.                                                                                                     |

The parser now enforces the runner's maximum evaluated expression-tree depth of 50. Adjacent identical logical operators are counted as one level, matching the runner's flattened And/Or representation; parentheses add no tree node. This fixes upstream mixed function/logical depth fixtures that became incorrectly accepted after correcting the format arity. Boundary tests cover unary operators, property/index access, function calls, mixed nodes, and long valid logical chains.

Runtime coercions are not all desirable lint acceptances. Existing intentional checks against object/array/null interpolation and suspicious scalar coercion remain. This audit does not convert every runtime fallback into a claim of recommended valid workflow syntax. Unknown runtime matrix/permissions behavior likewise remains outside automatic defect classification.

Builtin context property types retain existing documentation-backed definitions; this patch changes availability and visit coverage. Strict step/needs/input object typing continues to supply semantic lint diagnostics beyond schema shape validation.

## Scalar inventory and boundaries

- Boolean grammar: true/True/TRUE and false/False/FALSE. True and TRUE previously decoded as false; parser correction is tested against actual workflow AST values.
- Integer grammar: unsigned decimal, signed decimal, lowercase 0x hexadecimal, lowercase 0o octal. Leading zeros retain decimal interpretation. Binary, underscores, signed hexadecimal, and signed octal are not core spellings.
- Float grammar: decimal/exponent forms plus the reader's explicit `.inf`/`.nan` case variants. Radix parsing is grammar-constrained; a generic go-yaml Decode would incorrectly accept additional YAML 1.1 spellings.
- Numeric bounds and finite timeout/max-parallel policies remain parser concerns. Tests verify `0x1e`, `0o36`, `030`, and `3e1` all produce 30 for timeout-minutes. Nonfinite values are not asserted to be valid timeouts.
- Quoted scalars preserve string identity in matrix typing; expression booleans/numbers use their computed types. Input default/call type checks and arbitrary matrix object typing remain existing semantic policies.

## Verification

`go test . ./scripts/generate-availability -run 'TestSchemaAudit|TestWorkflowKeyAvailability|TestSpecialFunctionNames|TestExprSemanticsCheck|TestExprBuiltinFunctionSignatures|TestOKWrite' -count=1` passes.

`schema_context_audit_test.go` includes complete availability profiles, special-function boundary and position tests, scalar AST checks, static required-flag rejections, and full visitor regressions for snapshots/background/static shell fields. Generator expected output was regenerated from its checked-in documentation fixture. Complete-suite results are reported by the integration audit; the command above is focused local verification. Remote CI was not run.

Additional focused verification covers valid and invalid evaluated mapping/union values, matrix inference, unknown dynamic values, and the corrected existing expression fixture expectations. `rule_expression_schema.go` centralizes these evaluated-value contracts. It preserves optional properties and schema scalar-to-string conversion without treating truthiness as boolean assignability.

Known `fromJSON` and string-literal values are retained for constraints lost by type merging: every heterogeneous array element is checked, declared nonempty strings remain nonempty, and concurrency queue enum/conflict rules apply. Thus `ports: [80, {}]` cannot evade checks by merging its inferred element type to `any`. Unknown computed values continue to be deferred. Empty environment names and absent job-container images are not relabeled as schema violations merely because literal-form lint policy is stricter.
