# GitHub Actions expression behavior

## What to watch for

### Condition source is different from a computed string

An empty, null or whitespace-only `if:` becomes `success()`.

| Condition source                    | Meaning                           |
| ----------------------------------- | --------------------------------- |
| `${{ '' }}` or `${{ ' ' }}`         | `success()`                       |
| `${{ 'false' }}`                    | Boolean `false`; skips            |
| `${{ '1 == 2' }}`                   | False comparison; skips           |
| `${{ 'failure()' }}`                | Status function                   |
| `${{ 'nope' }}`                     | Invalid variable reference        |
| `${{ format('') }}`                 | Computed empty string; falsy      |
| `${{ format('false') }}`            | Computed non-empty string; truthy |
| `${{ env.FLAG }}` with `FLAG=false` | String data; truthy               |

`if: "null"` evaluates to null and skips. `if: null` becomes `success()`. The runner never reparses text from `env`, outputs or function calls.

### String flags can disagree with boolean comparisons

The output string `'0'` is truthy but equals boolean `false`. `if: steps.check.outputs.flag` can run while `if: steps.check.outputs.flag == true` skips. The string `'false'` is truthy too. Mixed-kind equality is not transitive. `'' == 0` and `0 == '0'` are true, but `'' == '0'` is false.

Compare against an explicit string:

```yaml
- if: ${{ steps.validate.outputs.allowed == 'true' }}
  run: echo 'Allowed'
```

String equality ignores case, so `'ADMIN' == 'admin'` is true. Compare case-sensitive values in a script and output `true` or `false`. The opt-in [suspicious checks](rules.md) flag bare string gates and comparisons with numeric or boolean literals.

### JSON member collisions depend on parsing mode

Object lookup ignores case. `fromJSON('{"approved":false,"APPROVED":true}')` holds two spellings of one member. Loose parsing keeps the last and strict parsing keeps the first. actionlint reports each colliding key with its JSON offset. It also rejects composite action outputs whose names differ only in case.

### Matrix expression arrays insert one level

An expression that returns an array inside a matrix sequence inserts the array's elements. `[before, ${{ fromJSON('["one","two"]') }}, after]` produces four values. A literal nested `[one, two]` counts as one value. `include` and `exclude` follow the same rule.

### The same digits can mean different numbers

The string `017` equals `17` in a comparison, but `fromJSON()` reads it as octal and returns `15`. `JSON.parse()` rejects it.

```yaml
name: Compare numeric interpretations
on: workflow_dispatch
permissions: {}
env:
  RAW_NUMBER: "017"
jobs:
  compare:
    runs-on: ubuntu-latest
    steps:
      - env:
          COMPARES_TO_17: ${{ env.RAW_NUMBER == 17 }}
          PARSED_VALUE: ${{ toJSON(fromJSON(env.RAW_NUMBER)) }}
        run: printf 'comparison=%s parsed=%s\n' "$COMPARES_TO_17" "$PARSED_VALUE"
```

Output:

```text
comparison=true parsed=15
```

An octal prefix works in a literal but fails in `fromJSON()`:

| Evaluated as             | Value supplied  | Result                                                                                                                             |
| ------------------------ | --------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| Expression literal       | `0o17`          | [`15`](expression-results/2026-09-14.md#literal-15)                                                                                |
| `fromJSON(inputs.value)` | String `"0o17"` | [Evaluation error](expression-results/2026-09-14.md#json-octal-prefix): `Unexpected character encountered while parsing number: o` |

### `fromJSON()` accepts more than JSON

| Text passed to `fromJSON()`      | Result                                                   | Evidence                                                                |
| -------------------------------- | -------------------------------------------------------- | ----------------------------------------------------------------------- |
| `31 trailing`                    | `31`; trailing text is ignored                           | [Results](expression-results/2026-09-14.md#json-trailing-text-number)   |
| `{"n":1}{"n":2}`                 | Object with `n: 1`; the second object is ignored         | [Results](expression-results/2026-09-14.md#json-two-objects)            |
| `/* comment */{"n":1}`           | String `" comment "`; `.n` is `null`                     | [Results](expression-results/2026-09-14.md#json-leading-comment-object) |
| `{"approved":/*x*/true,"n":1}`   | Object with `approved: true` and `n: 1`                  | [Results](expression-results/2026-09-14.md#json-comment-before-value)   |
| `{"role":"user","role":"admin"}` | Object with `role: "admin"`; the last duplicate key wins | [Results](expression-results/2026-09-14.md#json-duplicate-role)         |

`JSON.parse()` rejects the first four inputs and also keeps the last duplicate key.

### A negated rejection can accidentally allow a value

```javascript
const n = Number('not a number');
n; // NaN
n > 100; // false
n <= 100; // false
!(n > 100); // true
n >= 0 && n <= 100; // false
```

A guard written as "not over the limit" passes a value that was never a number. GitHub expressions behave the same:

```yaml
name: Compare NaN conditions
on: workflow_dispatch
permissions: {}
env:
  RAW_NUMBER: "NaN"
jobs:
  compare:
    runs-on: ubuntu-latest
    steps:
      - env:
          OVER_LIMIT: ${{ fromJSON(env.RAW_NUMBER) > 100 }}
          NOT_OVER_LIMIT: ${{ !(fromJSON(env.RAW_NUMBER) > 100) }}
          IN_RANGE: ${{ fromJSON(env.RAW_NUMBER) >= 0 && fromJSON(env.RAW_NUMBER) <= 100 }}
        run: |
          printf 'over_limit=%s\n' "$OVER_LIMIT"
          printf 'not_over_limit=%s\n' "$NOT_OVER_LIMIT"
          printf 'in_range=%s\n' "$IN_RANGE"
```

Output:

```text
over_limit=false
not_over_limit=true
in_range=false
```

Check the type and range first:

```javascript
function inRange(n) {
  return typeof n === 'number' && Number.isFinite(n) && n >= 0 && n <= 100;
}

inRange(NaN); // false
inRange(Infinity); // false
inRange('50'); // false: the argument is a string
inRange(50); // true
inRange(101); // false
```

### A missing property can satisfy a comparison

GitHub resolves a missing property to `null`, and `null` equals `false`, `0` and `''`:

```text
GitHub expression                          Result
fromJSON('{}').approved == false            true
fromJSON('{}').n == 0                       true
fromJSON('{}').role == ''                   true
```

`.approved == false` cannot tell a denial from a missing field. JavaScript differs:

```javascript
const input = JSON.parse('{}');
input.approved; // undefined
input.approved == false; // false
null == false; // false
null == 0; // false
null == ''; // false
```

### Large numbers can lose information before comparison

JavaScript rounds integers above `9007199254740991`:

```javascript
JSON.parse('9007199254740993'); // 9007199254740992
9007199254740993 === 9007199254740992; // true
Number.isSafeInteger(JSON.parse('9007199254740993')); // false
```

GitHub also evaluates `9007199254740993 == 9007199254740992` as `true`. Keep numeric identifiers as strings.

`1e309` is valid JSON and parses to `Infinity`:

```javascript
const n = JSON.parse('1e309');
n; // Infinity
Number.isFinite(n); // false
```

GitHub's `fromJSON()` returns `Infinity` too.

### A declared `number` input is not a substitute for validation

For an input declared as `type: number`, the workflow-dispatch API accepted strings and rejected a JSON number:

| Submitted JSON value | Dispatch                                    | `toJSON(inputs.value)` | `toJSON(github.event.inputs.value)` | Both `== 31` comparisons | Evidence                                                          |
| -------------------- | ------------------------------------------- | ---------------------- | ----------------------------------- | ------------------------ | ----------------------------------------------------------------- |
| `"31"`               | Accepted                                    | `"31"`                 | `"31"`                              | `true`                   | [Results](expression-results/2026-09-14.md#input-string-31)       |
| `"0x1F"`             | Accepted                                    | `"0x1F"`               | `"0x1F"`                            | `true`                   | [Results](expression-results/2026-09-14.md#input-string-0x1f)     |
| `"Infinity"`         | Accepted                                    | `"Infinity"`           | `"Infinity"`                        | `false`                  | [Results](expression-results/2026-09-14.md#input-string-infinity) |
| `"NaN"`              | Accepted                                    | `"NaN"`                | `"NaN"`                             | `false`                  | [Results](expression-results/2026-09-14.md#input-string-nan)      |
| `31`                 | HTTP 422: `Invalid value for input 'value'` | No run                 | No run                              | No run                   | [Response](expression-results/2026-09-14.md#input-number-31)      |

## An example that validates before making a decision

The `validate` step accepts only an object with a Boolean `approved`, a string `role` and an integer `n` from 0 to 100. It writes `allowed=true` only for an approved `admin`.

```yaml
name: Validate decision input
on:
  workflow_dispatch:
    inputs:
      payload:
        type: string
        required: true
permissions: {}
jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - id: validate
        env:
          RAW_INPUT: ${{ inputs.payload }}
        shell: bash
        run: |
          node <<'NODE'
          const fs = require('node:fs');
          const input = JSON.parse(process.env.RAW_INPUT);
          if (typeof input !== 'object' || input === null || Array.isArray(input)) {
            throw new Error('Expected an object');
          }
          if (typeof input.approved !== 'boolean' || typeof input.role !== 'string') {
            throw new Error('Expected approved: boolean and role: string');
          }
          if (!Number.isSafeInteger(input.n) || input.n < 0 || input.n > 100) {
            throw new Error('Expected integer n in [0, 100]');
          }
          const allowed = input.approved === true && input.role === 'admin';
          fs.appendFileSync(process.env.GITHUB_OUTPUT, `allowed=${allowed}\n`);
          NODE
      - if: ${{ steps.validate.outputs.allowed == 'true' }}
        run: echo 'Validated decision allows this step'
```

With payload `{"approved":true,"role":"admin","n":1}`, the second step prints:

```text
Validated decision allows this step
```

| Payload                                      | Validation result                                    | Next step          |
| -------------------------------------------- | ---------------------------------------------------- | ------------------ |
| `{"approved":false,"role":"admin","n":1}`    | Step output `allowed=false`                          | Skipped            |
| `{"approved":true,"role":"ADMIN","n":1}`     | Step output `allowed=false`                          | Skipped            |
| `{"role":"admin","n":1}`                     | Error: `Expected approved: boolean and role: string` | Skipped; job fails |
| `{"approved":true,"role":"admin","n":"1"}`   | Error: `Expected integer n in [0, 100]`              | Skipped; job fails |
| `{"approved":true,"role":"admin","n":1e309}` | Error: `Expected integer n in [0, 100]`              | Skipped; job fails |

## Run the reference yourself

The probe creates temporary workflow refs and consumes Actions runs. It needs Node.js 26 and a `gh` login with Contents, Workflows and Actions write access. `GITHUB_TOKEN` cannot create the refs. `.github/workflows/expr-conformance-probe.yml` must already exist on the default branch.

```sh
# Inspect the cases without network access.
node internal/development/tests/conformance/expressions/probe.ts list

# Capture the full corpus into a new directory.
node internal/development/tests/conformance/expressions/probe.ts run \
  --repo OWNER/REPO --output .cache/expression-probe/my-capture

node internal/development/tests/conformance/expressions/probe.ts run \
  --repo OWNER/REPO --output .cache/expression-probe/json-capture --group from-json
node internal/development/tests/conformance/expressions/probe.ts run \
  --repo OWNER/REPO --output .cache/expression-probe/one-case --case json-leading-zero

# Recollect evidence for existing run IDs without dispatching again.
node internal/development/tests/conformance/expressions/probe.ts collect --output .cache/expression-probe/my-capture

# Finish cleanup if the driver was interrupted.
node internal/development/tests/conformance/expressions/probe.ts cleanup --output .cache/expression-probe/my-capture
```

A successful full capture ends with:

```text
Complete evidence: /path/to/actionlint/.cache/expression-probe/my-capture
```

The output directory holds `summary.md` and `results.json`. The **Expression conformance probe** workflow runs the same capture from the Actions tab and needs a repository secret with the same access. `.github/workflows/expr-conformance-probe.yml` names that secret.

## Read the evidence

The 14 September 2026 capture has a [result table](expression-results/2026-09-14.md), [structured data](expression-results/2026-09-14.json) and an [archive of workflows and logs](expression-results/2026-09-14.zip). One literal differs by location. [`toJSON(-0)`](expression-results/2026-09-14.md#literal-02) produced `0` in the run title and `-0` on the runner.
