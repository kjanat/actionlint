# 0016 — Expression typing: flow-typed from declarations, runner semantics, user annotations

Status: accepted · 2026-10-28 · operator semantics read from `actions/runner` `src/Sdk/DTExpressions2` at `80bb1fb8`

## Context

v1 (Go, `internal/expression` at `f829a4ea`, ~3.1k lines) carried a deep
expression type checker and it was the most valuable thing it did. Read
from source, it already had:

- strict vs loose object types (`Mapped: nil` vs `AnyType`); `steps`,
  `matrix`, `needs`, `inputs`, `job`, `runner` strict, `github.event`
  loose. Properties stored case-insensitively with the declared spelling
  kept (`SetProperty`)
- `fromJSON(<string literal>)` → `TypeOfJSONValue` on the parsed value, so
  a constant JSON object was a strict type and a missing member was
  reported
- `JSONMemberCollisions` on that literal with an `OrdinalIgnoreCase` fold
  that correctly refuses to equate non-ASCII with ASCII (dotless i, long
  s)
- `EventVariant`s: `github.event` typed as the merge of the `on:`
  events' payloads, with **guard narrowing** on
  `github.event_name == '…'`
- `ConditionConstantValue` folding, incl. negative zero; `string-conditions`;
  `CheckUnsoundTernaries` with `case`-aware truthiness, and an
  `UntrustedInputMap` for script-injection paths

What it did **not** have: a type for `fromJSON(<non-literal>)`. Anything
parsed from a context value — `needs.x.outputs.json`, `inputs.config`,
`github.event.comment.body` — became `any`, so a missing member there was
invisible, and at runtime the runner silently yields `null` for a missing
property (`Index.cs` returns `null`, never throws). Closing that hole is
what the annotation design below is for. The rest is kept as a
**capability floor** (ADR-0001: no code is ported. The list above is what
v2 must not regress below).

The runner's actual semantics, from source, that any typing must honour:

| fact                                                                                                                                                                                                               | source                                                             |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------ |
| ``&&`` / `                                                                                                                                                                                                         |                                                                    | ``return an *operand value*, not a boolean.``a && b``→``a``if falsy else``b``. ``a \|\| b``→``a``if truthy else``b` | `Operators/And.cs`, `Or.cs` |
| falsy = `null`, `false`, `0`, `NaN`, `""`. Everything else truthy, **including the string `"false"`**                                                                                                              | `EvaluationResult.IsFalsy`                                         |
| `==`/`!=` coerce: number↔string via `ParseNumber`; boolean/null → number (`true`→1, `null`→0) then re-coerce; strings compare **case-insensitively**; object/array by reference; kinds that still differ → `false` | `EvaluationResult.AbstractEqual`, `CoerceTypes`, `ConvertToNumber` |
| property/index on `null` or a missing key → `null`                                                                                                                                                                 | `Operators/Index.cs`                                               |
| `case(p1, v1, …, default)`: odd arity; **each predicate must be `Boolean` kind or the evaluation throws** `"case predicate must evaluate to a boolean value"`                                                      | `Functions/Case.cs`                                                |
| `fromJSON` leniency = Json.NET, no trailing check                                                                                                                                                                  | `Functions/FromJson.cs` (ADR-0014)                                 |
| function and context names case-insensitive; depth 50; length 21000                                                                                                                                                | ADR-0014                                                           |

## Decision

### Lattice (`model::Type`)

```
null | bool | number | string | 'lit' | 'a' | 'b' …   (string literal unions)
array<T> | object { k: T, k?: T, … } (closed) | map<T> (open)
json<T>      a string whose fromJSON is T
any          opaque, produces no diagnostics
never        provably absent (closed object, unknown key)
T | U        unions, normalised
```

Literal unions are first-class so `runs-on`, `matrix.os`, `needs.x.result`
and `runner.os` type as finite sets and rules enumerate them.

### Sources, in `semantics::type_env(OccurrenceKey)`

| expression                                                                        | type comes from                                                                                                                                                                                                                                                                                                                                                                                             |
| --------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| any name                                                                          | present iff in the field's `context` array (ADR-0014); else `expression/unknown-context`                                                                                                                                                                                                                                                                                                                    |
| `github.*`                                                                        | `context-providers/github.ts`; `github.event.*` → `webhooks.json` payload tree, union over the document's `on:` events and activity types (names only → `map<any>` leaves). **Guard narrowing** (v1 `EventVariant`): inside `if: github.event_name == 'push'` or `github.event.action == 'opened'`, and in a job/step whose enclosing `if` establishes it, `github.event` narrows to that variant's payload |
| `inputs.*`                                                                        | `workflow_dispatch` / `workflow_call` input definitions: `string`, `boolean`, `number`, `choice` → literal union of `options`, `environment` → `string`; `required: false` without `default` → `T \| null`                                                                                                                                                                                                  |
| `needs.<job>.outputs.*`                                                           | the job's `outputs:` mapping, or the called workflow's `on.workflow_call.outputs` through `invocation_graph`; unknown key → `never`                                                                                                                                                                                                                                                                         |
| `needs.<job>.result`                                                              | `'success' \| 'failure' \| 'cancelled' \| 'skipped'`                                                                                                                                                                                                                                                                                                                                                        |
| `steps.<id>.outputs.*`                                                            | resolved `action.yml` `outputs` (via `resolve_uses`); `run` steps → `map<string>` unless annotated; unknown `<id>` → `never`                                                                                                                                                                                                                                                                                |
| `steps.<id>.outcome` / `.conclusion`                                              | same literal union as `result`                                                                                                                                                                                                                                                                                                                                                                              |
| `matrix.<k>`                                                                      | union of the literal values incl. `include`/`exclude`; expression-valued matrix → `any` unless annotated                                                                                                                                                                                                                                                                                                    |
| `env.*`                                                                           | declared `env` at workflow/job/step scope → `string`; undeclared → `string \| null`                                                                                                                                                                                                                                                                                                                         |
| `secrets.*`, `vars.*`                                                             | with host facts (ADR-0018): `secrets` is a closed object of the names visible to the job (repo ∪ org ∪ the job's `environment`), each `string`; `vars` is a closed object whose members are **string literal types** from the fetched values. Without a token: `string`, names from config `[types]` when declared, else open                                                                               |
| `runner.os`, `runner.arch`                                                        | `'Linux' \| 'Windows' \| 'macOS'`, `'X86' \| 'X64' \| 'ARM' \| 'ARM64'` (`context-providers/*.ts`)                                                                                                                                                                                                                                                                                                          |
| `fromJSON(e)`                                                                     | if `e : json<T>` → `T`; if `e` is a constant string → type of the parsed literal (Json.NET rules); else `any`                                                                                                                                                                                                                                                                                               |
| `toJSON`, `format`, `join`, `hashFiles`                                           | `string` (`join` of a string literal union stays a union only when all parts are literal)                                                                                                                                                                                                                                                                                                                   |
| `contains`, `startsWith`, `endsWith`, `always`, `success`, `failure`, `cancelled` | `bool`                                                                                                                                                                                                                                                                                                                                                                                                      |
| `case(p…, v…, d)`                                                                 | union of all `v` and `d`; each `p` must type `bool`, else `expression/case-predicate-not-boolean` (**correctness**, runtime throw)                                                                                                                                                                                                                                                                          |
| `a && b`, `a \|\| b`                                                              | operand unions narrowed by truthiness: `a && b : falsy(a) \| b`, `a \|\| b : truthy(a) \| b`                                                                                                                                                                                                                                                                                                                |
| `!a`, comparisons                                                                 | `bool`                                                                                                                                                                                                                                                                                                                                                                                                      |
| `x.k` / `x['k']`                                                                  | field type; on closed `object` without `k` → `never` + `expression/unknown-property`; on `null` → `null` + `expression/index-on-null`                                                                                                                                                                                                                                                                       |

### Diagnostics this enables (all `expression/*`)

| rule                                             | category    | fires when                                                                                                                                                                                       |
| ------------------------------------------------ | ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `unknown-property`                               | suspicious  | closed object type lacks the key (`fromJSON` with annotation, action outputs, workflow outputs, event payload)                                                                                   |
| `index-on-null`                                  | suspicious  | receiver type is exactly `null`                                                                                                                                                                  |
| `case-predicate-not-boolean`                     | correctness | predicate type excludes `bool`; source `Case.cs`                                                                                                                                                 |
| `compare-always-false`                           | suspicious  | `==` between kinds that `AbstractEqual` can never equate (object vs string, array vs number)                                                                                                     |
| `string-truthiness`                              | suspicious  | a `string`-typed value used as a condition where `"false"` would be truthy (`if: inputs.flag` with a string input)                                                                               |
| `ternary-idiom`                                  | style       | `c && a \|\| b`. Fix → `case(c, a, b)`: `Safe` iff `c : bool` and `a` cannot be falsy; `Unsafe` otherwise (behaviour differs when `a` is falsy, or `c` is non-boolean and would throw in `case`) |
| `unknown-context` / `unknown-function` / `arity` | correctness | ADR-0014                                                                                                                                                                                         |

`runner/unknown-label` and `matrix/*`, `needs/*`, `call/*` consume the
literal-union types instead of pattern-matching expressions. An expression
that types to plain `string` is simply not reported.

### Measured semantics folded in (ADR-0017, runny)

- **`if:` compilation is part of the model.** `ConvertToIfCondition`:
  null/blank/whitespace → `success()` (step *runs*). Otherwise
  `success() && (<expr>)` unless the expression names a status function.
  A single string-literal expression `${{ 'x' }}` is folded to the text
  `x` and re-parsed as condition source: `${{ 'false' }}` skips,
  `${{ 'failure()' }}` runs on failure, `${{ 'nope' }}` rejects the
  workflow. Bare YAML `True`/`False`/`Null` are normalised before
  parsing. Quoted `"True"` is an unrecognised name. The `expression/*`
  rules run on the *compiled* condition.
- **`map<T>` keys are case-insensitive** (`fromJSON` dictionaries,
  `steps.*`, `github.*`). The type keeps every source occurrence so
  `expression/json-key-collision` can name exact and case collisions
  together. Lookups resolve as the loose Json.NET path does (exact
  duplicates collapse last-wins in place, then case-fold assigns the last
  value under the first spelling).
- **String comparison is `OrdinalIgnoreCase`.** `expression/
  case-insensitive-gate` fires when `==`/`!=`/`startsWith`/`endsWith`/
  `contains` compare a literal against a context whose value is
  attacker-namable (`github.head_ref`, `github.ref`,
  `github.event.*.name`, `github.event.deployment.environment`).
- **Comparison kind is chosen per operand pair.** `<`/`>` are not
  transitive across mixed kinds. `==` is not transitive (`'' == 0`,
  `0 == '0'`, `'' != '0'`). `expression/cross-type-compare` fires on a
  `string`-typed operand against a `number`/`bool` literal. Hex literals
  read through `Int32.TryParse` (`0x80000000` is negative).
- **Composite status functions.** In a composite *main* step,
  `success()`/`failure()` read the action's status, `cancelled()` the
  job's. `type_env` carries `(enclosing_action, stage)` so the rule for
  `step/status-function-in-composite` can say which status is consulted.
- **Leading-zero numbers.** The public SDK reads `010` as octal, the
  hosted service as decimal. `expression/leading-zero-number`
  (suspicious): both verdicts are recorded, neither is trusted.

### Annotations

Annotate **declarations**, not uses. A `fromJSON` call then types itself
from the string it receives.

```yaml
on:
  workflow_call:
    outputs:
      jobs:
        # @type json<map<bool>>
        value: ${{ jobs.changes.outputs.jobs }}
```

```yaml
- id: plan
  # @type outputs: { platforms: json<{ os: string, arch: 'x64' | 'arm64' }[]>, count: number }
  run: node plan.mjs >> "$GITHUB_OUTPUT"
```

```toml
[types]
"vars.PROVENANCE"              = "'true' | 'false'"
"secrets.CARGO_REGISTRY_TOKEN" = "string"
"github.event.client_payload"  = "{ tag: string, dry-run?: bool }"  # repository_dispatch
```

- Comment form: `# @type <T>` as the line immediately above, or trailing
  on, a declaration key (`outputs.<name>`, `inputs.<name>`, an `env` key,
  a step with `id`, a matrix key). `# @type outputs: {…}` on a step
  types all its outputs at once.
- Grammar for `T`: the lattice above, TypeScript-flavoured. Plus
  `$ref("path.json")` → JSON Schema (draft 2020-12 subset) converted to
  a type, for payloads that already have a schema.
- Annotations are **assertions about the producer**. Where the producer
  is also in the repo (a called workflow, a local action), the annotation
  is checked against the producer's declared outputs → `type/annotation-mismatch`.
- Why comments, not a pre-`---` document: GitHub's reader sees the whole
  file. A second YAML document before the real one would have to be
  proven ignored by the server-side parser, which is closed
  (ADR-0014). Comments are proven inert.

### What `any` means

`any` is a *conscious hole*: unannotated `fromJSON`, expression-valued
matrices, `github.event.*` leaves. Rules never report on `any`. The
`type/untyped-fromjson` rule (`pedantic`, off) lists every `any` source
so a project that wants full coverage can annotate its way to zero.

## Consequences

- `type_env` is one salsa query per `OccurrenceKey`. It depends on
  `invocation_graph` and `resolve_uses` only for `needs`/`steps` outputs,
  so a document with no reusable calls types without resolution.
- The `expression/*` family becomes the largest. Most of it is
  table-driven from this ADR's two tables.
- Fix applicability for `ternary-idiom` is the first place type
  information decides `Safe` vs `Unsafe` (ADR-0007).
- Annotation comments are CST nodes and survive formatting.

## Open

- Union width limit before collapsing to `string` (matrix of 256 cells
  × `include`): propose 64.
- Whether `# @type` should also accept the languageservices
  `descriptions.json` property descriptions for hover. Cosmetic, later.
- `$ref` JSON-Schema subset boundary: `type`, `properties`, `required`,
  `items`, `enum`, `oneOf` of literals. No `$dynamicRef`, no
  `unevaluated*`.
