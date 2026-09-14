# GitHub Actions expression behavior

Expressions decide which steps run and how workflow inputs are interpreted. A condition can look reasonable and still run a step you expected to skip: the string `'false'` is truthy, missing properties can compare equal to `false`, and the same numeric text can acquire different values in different parsers.

If your workflows use step outputs, supplied inputs or JSON to make decisions, this guide explains the pitfalls and shows how to validate values before using them. Each example connects the observed behavior to a practical change you can make in your workflow. The [probe corpus](../scripts/expression-probe/cases.ts) and [measurement workflow](../.github/workflows/expr-conformance-probe.yml) let you reproduce the results yourself.

The key distinction is where a value is interpreted. An expression literal, a string used in a comparison, a `fromJSON()` argument, and a declared `number` input do not necessarily pass through the same parser. The examples below follow those differences through to the resulting workflow decisions.

The [14 September 2026 measurements](expression-results/2026-09-14.md) contain the full results, generated workflows and logs. The examples below link to those saved results.

## What to watch for

### The same digits can mean different numbers

The string `017` compares equal to decimal `17` through implicit expression coercion. Passing that string through `fromJSON()` produces `15`. JavaScript's `JSON.parse()` rejects it as invalid JSON.

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

The comparison treats `017` as decimal `17`, while `fromJSON()` reads it as octal `17`, which is decimal `15`.

The measured [string comparison](expression-results/2026-09-14.md#coercion-strings) and [`fromJSON()` evaluation](expression-results/2026-09-14.md#json-leading-zero) show both results. The latter also records JavaScript's parser error: `Unexpected number in JSON at position 1 (line 1 column 2)`.

The accepted syntax differs too. An octal prefix works as an expression literal but fails as a `fromJSON()` input:

| Evaluated as             | Value supplied  | Result                                                                                                                             |
| ------------------------ | --------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| Expression literal       | `0o17`          | [`15`](expression-results/2026-09-14.md#literal-15)                                                                                |
| `fromJSON(inputs.value)` | String `"0o17"` | [Evaluation error](expression-results/2026-09-14.md#json-octal-prefix): `Unexpected character encountered while parsing number: o` |

Do not use a successful comparison as proof that another parser will interpret the input the same way. Choose a representation and validate it before performing decisions or passing it between tools. Decimal integers without leading zeros avoid this particular ambiguity.

### `fromJSON()` accepts more than JSON

The corpus tests hexadecimal numbers, leading and trailing decimal points, single quotes, unquoted property names, trailing commas, comments, duplicate keys, and trailing content. Some of these change which value the workflow receives:

| Text passed to `fromJSON()`      | Result                                                   | Evidence                                                                |
| -------------------------------- | -------------------------------------------------------- | ----------------------------------------------------------------------- |
| `31 trailing`                    | `31`; trailing text is ignored                           | [Results](expression-results/2026-09-14.md#json-trailing-text-number)   |
| `{"n":1}{"n":2}`                 | Object with `n: 1`; the second object is ignored         | [Results](expression-results/2026-09-14.md#json-two-objects)            |
| `/* comment */{"n":1}`           | String `" comment "`; `.n` is `null`                     | [Results](expression-results/2026-09-14.md#json-leading-comment-object) |
| `{"approved":/*x*/true,"n":1}`   | Object with `approved: true` and `n: 1`                  | [Results](expression-results/2026-09-14.md#json-comment-before-value)   |
| `{"role":"user","role":"admin"}` | Object with `role: "admin"`; the last duplicate key wins | [Results](expression-results/2026-09-14.md#json-duplicate-role)         |

JavaScript's `JSON.parse()` rejects the first four inputs. It accepts the duplicate-key input and also keeps the last value.

When input must be JSON, validate the entire document with a JSON parser and validate its shape and values. A `fromJSON()` call succeeding is not that validation. If duplicate keys must be forbidden, select a parser that explicitly rejects them.

### A negated rejection can accidentally allow a value

`NaN` means "not a number". A failed numeric conversion is one way to obtain it. In a JavaScript console, these expressions evaluate as shown:

```javascript
const n = Number('not a number');
n; // NaN
n > 100; // false
n <= 100; // false
!(n > 100); // true
n >= 0 && n <= 100; // false
```

`n > 100` and `n <= 100` are both false because neither comparison establishes an ordering for `NaN`. Negating the first result produces `true`; it does not establish that the number is at most 100. A step guarded by "not over the limit" can therefore run for a value that was never a valid number.

GitHub expressions exhibit the same comparison behavior. Here is a workflow that prints the three decisions for a value returned by `fromJSON()`:

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

The [`NaN` object probe](expression-results/2026-09-14.md#json-nan-approved) records these comparisons for a parsed property, and the [decision probe](expression-results/2026-09-14.md#decision-pitfalls) records them for the `NaN` literal.

Check the input's type and allowed range before using it. In JavaScript, that can look like this:

```javascript
function inRange(n) {
  return typeof n === 'number' && Number.isFinite(n) && n >= 0 && n <= 100;
}

inRange(NaN); // false
inRange(Infinity); // false
inRange('50'); // false: a string, not a number
inRange(50); // true
inRange(101); // false
```

### A missing property can satisfy a comparison

Suppose a workflow receives `{}` where it expected an `approved` property. GitHub resolves the missing property to `null`. Its loose equality then gives these results, captured by the [decision probe](expression-results/2026-09-14.md#decision-pitfalls):

```text
GitHub expression                          Result
fromJSON('{}').approved == false            true
fromJSON('{}').n == 0                       true
fromJSON('{}').role == ''                   true
```

A check such as `.approved == false` cannot distinguish an explicit denial from a missing field. JavaScript handles missing properties and null equality differently:

```javascript
const input = JSON.parse('{}');
input.approved; // undefined
input.approved == false; // false
null == false; // false
null == 0; // false
null == ''; // false
```

Validate required properties explicitly. For example, `typeof input.approved === 'boolean'` rejects the missing field and accepts only an actual Boolean. Translating a JavaScript condition into a GitHub expression without checking these semantics can change the decision.

### A string that says false is still truthy

Environment variables and step outputs are strings. Testing whether they are truthy does not interpret their contents as a Boolean. These JavaScript expressions illustrate the distinction:

```javascript
'false' && 'runs' || 'skips'; // 'runs'
false && 'runs' || 'skips'; // 'skips'
true && 0 || 'fallback'; // 'fallback'
```

The [GitHub decision probe](expression-results/2026-09-14.md#decision-pitfalls) produces the same three results. The first expression runs because the string is nonempty. The last selects the fallback because `0` is falsy, even though the condition is true. The `condition && value || alternative` construction therefore needs a truthy middle value.

For a step output you define as the exact strings `true` and `false`, test it explicitly:

```yaml
- if: ${{ steps.validate.outputs.allowed == 'true' }}
  run: echo 'Allowed'
```

With `allowed=true`, this step prints `Allowed`. With `allowed=false`, it is skipped. The complete validation example below shows how to produce that output.

### GitHub string equality ignores case

This is a difference from JavaScript. In JavaScript:

```javascript
'ADMIN' == 'admin'; // false
'ADMIN' === 'admin'; // false
```

In the [GitHub expression evaluator](expression-results/2026-09-14.md#decision-pitfalls):

```text
'ADMIN' == 'admin'    -> true
```

Perform case-sensitive decisions in code when exact spelling matters, then return a canonical Boolean output to the workflow.

### Large numbers can lose information before comparison

JavaScript numbers cannot represent every integer above `9007199254740991` exactly. Parsing a larger integer can round it before your condition ever runs:

```javascript
JSON.parse('9007199254740993'); // 9007199254740992
9007199254740993 === 9007199254740992; // true
Number.isSafeInteger(JSON.parse('9007199254740993')); // false
```

The [GitHub decision probe](expression-results/2026-09-14.md#decision-pitfalls) likewise records `true` for `9007199254740993 == 9007199254740992`. Keep numeric identifiers as strings and compare them in code when every digit matters.

Valid JSON can also exceed the numeric range entirely. In JavaScript:

```javascript
const n = JSON.parse('1e309');
n; // Infinity
Number.isFinite(n); // false
```

The [overflow probe](expression-results/2026-09-14.md#json-overflow-approved) records `Infinity` in both JavaScript and GitHub's `fromJSON()`. Successful JSON parsing therefore still needs a finite-value or allowed-range check.

### A declared `number` input is not a substitute for validation

The typed-input probes submit both strings and a JSON number to the workflow-dispatch API. They capture `toJSON(inputs.value)`, `toJSON(github.event.inputs.value)`, and comparisons against `31`.

With the workflow input declared as `type: number`, the results were:

| Submitted JSON value | Dispatch                                    | `toJSON(inputs.value)` | `toJSON(github.event.inputs.value)` | Both `== 31` comparisons | Evidence                                                          |
| -------------------- | ------------------------------------------- | ---------------------- | ----------------------------------- | ------------------------ | ----------------------------------------------------------------- |
| `"31"`               | Accepted                                    | `"31"`                 | `"31"`                              | `true`                   | [Results](expression-results/2026-09-14.md#input-string-31)       |
| `"0x1F"`             | Accepted                                    | `"0x1F"`               | `"0x1F"`                            | `true`                   | [Results](expression-results/2026-09-14.md#input-string-0x1f)     |
| `"Infinity"`         | Accepted                                    | `"Infinity"`           | `"Infinity"`                        | `false`                  | [Results](expression-results/2026-09-14.md#input-string-infinity) |
| `"NaN"`              | Accepted                                    | `"NaN"`                | `"NaN"`                             | `false`                  | [Results](expression-results/2026-09-14.md#input-string-nan)      |
| `31`                 | HTTP 422: `Invalid value for input 'value'` | No run                 | No run                              | No run                   | [Response](expression-results/2026-09-14.md#input-number-31)      |

The quotes in the result columns matter: the accepted inputs remained strings in both contexts. A successful `== 31` comparison can therefore be numeric coercion of a string, including `"0x1F"`. Parse and validate the value where you use it.

## An example that validates before making a decision

This example consumes a JSON object with a Boolean `approved`, a case-sensitive `role`, and a bounded integer `n`. Input is passed through an environment variable. The script emits a canonical Boolean output only after validation.

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

With payload `{"approved":true,"role":"admin","n":1}`, validation writes the step output `allowed=true`. The next step runs and prints:

```text
Validated decision allows this step
```

Changing the payload illustrates the other paths:

| Payload                                      | Validation result                                    | Next step          |
| -------------------------------------------- | ---------------------------------------------------- | ------------------ |
| `{"approved":false,"role":"admin","n":1}`    | Step output `allowed=false`                          | Skipped            |
| `{"approved":true,"role":"ADMIN","n":1}`     | Step output `allowed=false`                          | Skipped            |
| `{"role":"admin","n":1}`                     | Error: `Expected approved: boolean and role: string` | Skipped; job fails |
| `{"approved":true,"role":"admin","n":"1"}`   | Error: `Expected integer n in [0, 100]`              | Skipped; job fails |
| `{"approved":true,"role":"admin","n":1e309}` | Error: `Expected integer n in [0, 100]`              | Skipped; job fails |

Adapt the accepted shape and range to the workflow.

## Run the reference yourself

The probe is manual because it creates temporary workflow refs and consumes GitHub Actions runs. Its generated workflows have `permissions: {}`, use no checkout or external actions, and receive only the controlled values in the corpus.

The driver needs Node.js 26 and an authenticated `gh` CLI with permission to create workflow files and dispatch/read runs in the chosen repository. For a fine-grained token, the relevant repository permissions are Contents: write, Workflows: write and Actions: write. The built-in `GITHUB_TOKEN` cannot create these workflow refs. The driver credential stays in the driver and is never passed to generated workflows.

The repository must already have `.github/workflows/expr-conformance-probe.yml` registered on its default branch. This requirement comes from GitHub's workflow-dispatch registration; a file that exists only on a new branch is insufficient.

```sh
# Inspect the cases without network access.
node scripts/expression-probe/probe.ts list

# Capture the full corpus into a new directory.
node scripts/expression-probe/probe.ts run \
  --repo OWNER/REPO --output .cache/expression-probe/my-capture

# Or measure one group / one case.
node scripts/expression-probe/probe.ts run \
  --repo OWNER/REPO --output .cache/expression-probe/json-capture --group from-json
node scripts/expression-probe/probe.ts run \
  --repo OWNER/REPO --output .cache/expression-probe/one-case --case json-leading-zero

# Recollect evidence for existing run IDs without dispatching again.
node scripts/expression-probe/probe.ts collect --output .cache/expression-probe/my-capture

# Finish cleanup if the driver was interrupted.
node scripts/expression-probe/probe.ts cleanup --output .cache/expression-probe/my-capture
```

After a full capture and cleanup succeed, the last console line is:

```text
Complete evidence: /path/to/actionlint/.cache/expression-probe/my-capture
```

Open `.cache/expression-probe/my-capture/summary.md` for the result table, or `results.json` in that directory for the structured data. Each case has its own subdirectory containing the generated workflow, dispatch response and, when it ran, its log.

For the hosted driver, configure the repository secret `EXPR_PROBE_TOKEN` and run **Expression conformance probe** from the Actions tab. Its artifact contains the same evidence as the local command.

A full capture makes up to 425 mutating API requests. Avoid launching multiple full captures in the same hour: GitHub's [secondary rate limits](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api#about-secondary-rate-limits) also account for other activity by the same user. The driver does not retry an uncertain dispatch, since doing so could create a duplicate run.

Cleanup removes the probe refs recorded in `refs.json` after checking their current SHAs. A changed ref is left for manual inspection.

## Read the evidence

The dated [result table](expression-results/2026-09-14.md), [structured data](expression-results/2026-09-14.json) and [archive of workflows and logs](expression-results/2026-09-14.zip) are stored in this repository. The archive includes the exact driver and corpus used for the capture, plus a SHA-256 manifest for every file. These saved records keep the examples usable when GitHub removes the hosted logs.

The corpus contains 85 probe cases:

| Group             | Cases | What is measured                                                                        |
| ----------------- | ----: | --------------------------------------------------------------------------------------- |
| Literals          |    44 | Numeric spellings, arithmetic and comparisons; service and runner results               |
| `fromJSON`        |    34 | Numbers, objects, comments and trailing content; parsed values and property comparisons |
| Coercion          |     1 | 13 strings, each compared in 12 ways, in a single workflow                              |
| Typed inputs      |     5 | Four string submissions and one JSON-number submission to a declared number input       |
| Decision examples |     1 | 12 truthiness, equality, range and precision checks                                     |

The 14 September capture produced 68 successful runs, two `fromJSON()` evaluation failures (`0o17` and `+1`), 14 expression-parser rejections and one input-validation rejection. All 85 temporary refs were removed after collection.

One literal has different serialized results between the two evaluation locations: [`toJSON(-0)`](expression-results/2026-09-14.md#literal-02) produced `0` in the service-rendered run title and `-0` on the runner. The other 29 accepted literal cases matched. This is why the capture preserves the text returned by each evaluator.

Each case includes its exact generated YAML and SHA-256, dispatch request, HTTP status, response body and diagnostic headers. Runs include their commit SHA, URL, attempt, timestamps and logs. Successful runner evaluations emit a JSON record. The value inside each record is the literal text produced by `toJSON()`: `NaN` and `Infinity` are preserved as text, not silently converted to JSON `null` or rounded by the collector.

For literal cases, the service-side value comes from the framed `display_title` rendered by `run-name`. Runner values come from step environment evaluation. The other groups measure runner evaluation and use a fixed run title so the runner receives every accepted dispatch.

The report distinguishes expression-parser rejection, dispatch-input rejection, workflow validation, runner evaluation failure and missing evidence. Unavailable logs, mismatched run identities and timeouts are reported as incomplete captures. Each run includes its runner, image and Node versions.

## Relationship to actionlint and upstream documentation

actionlint checks constant `fromJSON()` string arguments with a strict JSON parser. A literal argument such as `fromJSON('017')` can therefore be diagnosed even when GitHub accepts it at runtime. Dynamic input needs runtime validation. Deliberately invalid expressions are stored as test data and exercised through isolated probe workflows.

GitHub's [expression reference](https://docs.github.com/en/actions/reference/workflows-and-actions/expressions) describes the public language contract. Captures record the API version, runner versions and measurement times. A discrepancy can be reported with the relevant case, generated workflow and saved output.
