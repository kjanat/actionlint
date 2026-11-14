# All checks done by actionlint

This document lists the checks performed by [actionlint](..), with examples, output, and playground links.

List of checks:

- [Unexpected keys](#check-unexpected-keys)
- [Missing required keys or key duplicates](#check-missing-required-duplicate-keys)
- [Unexpected empty mappings](#check-empty-mapping)
- [Unexpected mapping values](#check-mapping-values)
- [Syntax check for expression `${{ }}`](#check-syntax-expression)
- [Type checks for expression syntax in `${{ }}`](#check-type-check-expression)
- [Contexts and built-in functions](#check-contexts-and-builtin-func)
- [Contextual typing for `steps.<step_id>` objects](#check-contextual-step-object)
- [Contextual typing for `matrix` object](#check-contextual-matrix-object)
- [YAML tags of matrix values](#check-matrix-value-tags)
- [Contextual typing for `needs` object](#check-contextual-needs-object)
- [Comparison coercions](#check-comparison-types)
- [shellcheck integration for `run:`](#check-shellcheck-integ)
- [Executable bits on repository scripts](#check-executable-bit)
- [Script injection by potentially untrusted inputs](#untrusted-inputs)
- [Job dependencies validation](#check-job-deps)
- [Parallel steps](#check-parallel-step-refs)
- [Matrix values](#check-matrix-values)
- [Webhook events validation](#check-webhook-events)
- [Workflow dispatch event validation](#check-workflow-dispatch-events)
- [Glob filter pattern syntax validation](#check-glob-pattern)
- [CRON syntax and IANA timezone string at `on.schedule`](#check-cron-syntax-and-timezone)
- [Runner labels](#check-runner-labels)
- [Action format in `uses:`](#check-action-format)
- [Local action inputs validation at `with:`](#check-local-action-inputs)
- [Popular action inputs validation at `with:`](#check-popular-action-inputs)
- [Action runtime lifecycle](#detect-outdated-popular-actions)
- [Shell name validation at `shell:`](#check-shell-names)
- [Job ID and step ID uniqueness](#check-job-step-ids)
- [Hardcoded credentials](#check-hardcoded-credentials)
- [Environment variable names](#check-env-var-names)
- [Cache access](#cache-mode)
- [Cache safety policies](#cache-policies)
- [Permissions](#permissions)
- [Reusable workflows](#check-reusable-workflows)
- [ID naming convention](#id-naming-convention)
- [Availability of contexts and special functions](#ctx-spfunc-availability)
- [Deprecated workflow commands](#check-deprecated-workflow-commands)
- [Constant conditions at `if:`](#if-cond-constant)
- [Action metadata syntax validation](#action-metadata-syntax)
- [Deprecated inputs usage](#deprecated-inputs-usage)
- [YAML anchors](#yaml-anchors)

The checks in this document run by default, including the configurable cache safety checks. Enable other policy checks
through [the configuration](config.md#policy-checks). Use a YAML checker such as [yamllint][yamllint]
to check formatting and general YAML style.

Additional [suspicious rules](config.md#stable-suspicious-rules) report surprising type conversions in conditions
and matrix filters. These rules are disabled by default and do not enforce project policies.

<a id="check-unexpected-keys"></a>

## Unexpected keys

Example input:

```yaml
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    # ERROR: Typo of `defaults:`
    default:
      run:
        working-directory: /path/to/dir
    steps:
      - run: echo hello
        # ERROR: `shell:` must be in lower case
        Shell: bash
permissions: {}
```

Output:

```console
test.yaml:6:5: unexpected key "default" for "job" section. expected one of "cache-mode", "cancel-timeout-minutes", "concurrency", "container", "continue-on-error", "defaults", "env", "environment", "if", "name", "needs", "outputs", "permissions", "runs-on", "secrets", "services", "snapshot", "steps", "strategy", "timeout-minutes", "uses", "with" [syntax-check]
  |
6 |     default:
  |     ^~~~~~~~
test.yaml:12:9: unexpected key "Shell" for step to run shell command. expected one of "background", "continue-on-error", "env", "id", "if", "name", "run", "shell", "timeout-minutes", "working-directory" [syntax-check]
   |
12 |         Shell: bash
   |         ^~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNo8jE2uwjAMBvc9xXeBqPtc450gbf1IIMSRf4QQ4u4oEWVlWzNjbhHdNS9X3jQugJHamIB40zC4b97MQ02DTXTQf/L69aZ5rsCD5VbaJRxFaDeWZ8Tak+XVeD2KTE+Nup5JmD1oz4xMtfLv1d84I7akeekk96JauGnE6/0ZAAB+N6Q=)

[Workflow syntax][syntax-doc] defines which keys each mapping can contain. Unknown keys cause the workflow run to fail.

actionlint reports unexpected keys while parsing a workflow. Key names are generally case-sensitive, although some
are case-insensitive. This check can catch mistakes in capitalization.

<a id="check-missing-required-duplicate-keys"></a>

## Missing required keys and key duplicates

Example input:

```yaml
on: push
jobs:
  test:
    strategy:
      # ERROR: Matrix name is duplicated. These keys are case-insensitive
      matrix:
        version_name: [v1, v2]
        VERSION_NAME: [V1, V2]
    # ERROR: runs-on is missing
    steps:
      - run: echo 'hello'
permissions: {}
```

Output:

```console
test.yaml:3:3: "runs-on" section is missing in job "test" [syntax-check]
  |
3 |   test:
  |   ^~~~~
test.yaml:8:9: key "VERSION_NAME" is duplicated in "matrix" section. previously defined at line:7,col:9. note that this key is case insensitive [syntax-check]
  |
8 |         VERSION_NAME: [V1, V2]
  |         ^~~~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNo8jL/KwkAQB/s8xa9Lk6/IV25nkcLCCAppgoQoi3eS+8Pt5lDEd5egsRqGgQmeEGcxxS2chQpAWXQhIJpG5evjY4AbNdn7akDmJDb4wY+OCX2uK+T/0y93zeG43bdDu9k1hL6rK3TfLMpR1tEf0uwJfDEBpeFpCmUROTkry10Iz9d7APQ0Lso=)

Some mappings must include specific keys. For example, job mappings must include `runs-on:` and `steps:`.

Duplicate keys are not allowed. Some workflow keys are compared **case-insensitively**: for example, job IDs
`test` and `TEST` cannot both appear in the same workflow. actionlint reports missing required keys and duplicates
while parsing.

<a id="check-empty-mapping"></a>

## Unexpected empty mappings

Example input:

```yaml
on: push
jobs:
permissions: {}
```

Output:

```console
test.yaml:2:6: "jobs" section should not be empty. please remove this section if it's unnecessary [syntax-check]
  |
2 | jobs:
  |      ^
```

[Playground](https://kjanat.github.io/actionlint/#eNrKz7NSKCgtzuDKyk8qtuIqSC3KzSwuzszPK7ZSqK4FDACmMgsC)

Some mappings and sequences, such as `jobs:`, must not be empty. The parser reports these errors.
GitHub rejects jobs without steps unless they call a reusable workflow.
The `empty-job` rule reports these jobs as errors. A step with `run: null` is valid;
`empty-run` warns that its script is empty.

<a id="check-mapping-values"></a>

## Unexpected mapping values

Example input:

```yaml
on: push
jobs:
  test:
    strategy:
      # ERROR: Boolean value "true" or "false" is expected
      fail-fast: off
      # ERROR: Integer value is expected
      max-parallel: 1.5
    runs-on: ubuntu-latest
    steps:
      - run: sleep 200
        # ERROR: Float value is expected
        timeout-minutes: two minutes
permissions: {}
```

Output:

```console
test.yaml:6:18: expecting a single ${{...}} expression or boolean literal "true" or "false", but found plain text node [syntax-check]
  |
6 |       fail-fast: off
  |                  ^~~
test.yaml:8:21: truncated: "max-parallel" value 1.5 is truncated to 1 [numeric-values]
  |
8 |       max-parallel: 1.5
  |                     ^~~
test.yaml:13:26: expecting a single ${{...}} expression or float number literal, but found plain text node [syntax-check]
   |
13 |         timeout-minutes: two minutes
   |                          ^~~
```

[Playground](https://kjanat.github.io/actionlint/#eNo0zF2KQjEMxfH3u4qzgQ53BuYlu8mFVCv9oklQEfcu1foUzp/wa5XQXc/bpR1KG2CiNi+gNtjkdP8sIHLKIbIaocW4YuFb6Dw4Z8mE35//dx9eNUzaD6/mIfNklypdv2SYnwTNIh1/+74yYKlIcwslVTdRgl0b1ti6jJJUU6tKeDxfAwD1zTt+)

Workflow fields can require a value from a fixed set of strings, a boolean, or a
number. actionlint checks literal values against the field's allowed values and
type, and reports values that do not meet those requirements.

<a id="check-syntax-expression"></a>

## Syntax check for expression `${{ }}`

Example input:

```yaml
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      # " is not available for string literal delimiter
      - run: echo '${{ "hello" }}'
      # + operator does not exist
      - run: echo '${{ 1 + 1 }}'
      # Missing ')' paren
      - run: echo "${{ toJson(hashFiles('**/lock', '**/cache/') }}"
      # unexpected end of input
      - run: echo '${{ github.event. }}'
permissions: {}
```

Output:

```console
test.yaml:7:24: invalid-syntax: got unexpected character '"' while lexing expression, expecting 'a'..'z', 'A'..'Z', '_', '0'..'9', ''', '}', '(', ')', '[', ']', '.', '!', '<', '>', '=', '&', '|', '*', ',', ' '. do you mean string literals? only single quotes are available for string delimiter [expression-syntax]
  |
7 |       - run: echo '${{ "hello" }}'
  |                        ^
test.yaml:9:26: invalid-syntax: got unexpected character '+' while lexing expression, expecting 'a'..'z', 'A'..'Z', '_', '0'..'9', ''', '}', '(', ')', '[', ']', '.', '!', '<', '>', '=', '&', '|', '*', ',', ' ' [expression-syntax]
  |
9 |       - run: echo '${{ 1 + 1 }}'
  |                          ^
test.yaml:11:65: invalid-syntax: unexpected end of input while parsing arguments of function call. expecting ",", ")" [expression-syntax]
   |
11 |       - run: echo "${{ toJson(hashFiles('**/lock', '**/cache/') }}"
   |                                                                 ^~
test.yaml:13:38: invalid-syntax: unexpected end of input while parsing object property dereference like 'a.b' or array element dereference like 'a.*'. expecting "IDENT", "*" [expression-syntax]
   |
13 |       - run: echo '${{ github.event. }}'
   |                                      ^~
```

[Playground](https://kjanat.github.io/actionlint/#eNp0zUGqgzAQxvG9p/gID+KzVXGbA3TRW2gYmrRpRpykG8ndS+za1Qz8f/BxNFizuObJi5gGSCSpXmDLUfra85Jjyn2YazuSJFrlp4C+SgOyjqH/9h3KUQisUIo+IxMumE6AqiDxXTi2bhZ384Gk1V03BrYvfUV97WwdjfofpaizkYdPLi8DfSim4RhbaXt7Ec9RDPbyHQA94UpL)

actionlint parses expressions in `${{ }}` using [the expression syntax][expr-doc]. It reports syntax errors such as
invalid characters, missing parentheses, and unexpected end of input.

<a id="check-type-check-expression"></a>

## Type checks for expression syntax in `${{ }}`

actionlint checks the types of expressions in `${{ }}` template placeholders. It supports these types:

| Type          | Description                                                                                | Notation                 |
| ------------- | ------------------------------------------------------------------------------------------ | ------------------------ |
| Any           | Any value like `any` type in TypeScript. Fallback type when a value can no longer be typed | `any`                    |
| Number        | Number value (integer or float)                                                            | `number`                 |
| Bool          | Boolean value                                                                              | `bool`                   |
| String        | String value                                                                               | `string`                 |
| Null          | Type of `null` value                                                                       | `null`                   |
| Array         | Array of specific type elements                                                            | `array<T>`               |
| Loose object  | Object which can contain any properties                                                    | `object`                 |
| Strict object | Object whose properties are strictly typed                                                 | `{prop1: T1, prop2: T2}` |
| Map object    | Object who has specific type values like `env` context                                     | `{string => T}`          |

actionlint checks whether a value has the type required by its workflow field.
Function arguments and comparisons use GitHub's [conversion rules](#check-comparison-types).
In a string template, booleans and numbers become text, and null becomes an empty string.
actionlint reports interpolated objects and arrays because the runner turns them into the
literal strings `Object` and `Array` rather than their contents.

Example input:

```yaml
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      # ERROR: `env` is object. Index access object is invalid
      - run: echo '${{ env[0] }}'
      # ERROR: Properties in objects are strongly typed. Missing property can be caught
      - run: echo '${{ job.container.os }}'
      # ERROR: `github.repository` is string. Trying to access .owner property is invalid
      - run: echo '${{ github.repository.owner }}'
      # ERROR: Objects and arrays cannot be interpolated as strings
      - run: echo '${{ env }}'
permissions: {}
```

Output:

```console
test.yaml:7:28: operand-type: property access of object must be type of string but got "number" [expression-type]
  |
7 |       - run: echo '${{ env[0] }}'
  |                            ^
test.yaml:9:24: unknown-property: property "os" is not defined in job.container [expression-reference]
  |
9 |       - run: echo '${{ job.container.os }}'
  |                        ^~~~~~~~~~~~~~~~
test.yaml:11:24: operand-type: receiver of object dereference "owner" must be type of object but got "string" [expression-type]
   |
11 |       - run: echo '${{ github.repository.owner }}'
   |                        ^~~~~~~~~~~~~~~~~~~~~~~
test.yaml:13:20: object and array values should not be evaluated in template with ${{ }} but evaluating the value of type {string => string} [expression]
   |
13 |       - run: echo '${{ env }}'
   |                    ^~~
```

[Playground](https://kjanat.github.io/actionlint/#eNp8yzEOwjAMheG9p3gDUqdGzLkKYmgqi6YCO4rtIlT17ihlptMb/u8JRxTXuVskaewAI7W2QHXWoXVPzubDc2ztSGpU9KeAockImmZBf9k2EK+36x373v8Ti6QwCduYmWoQPbOPbLOnUKmIZpP6CfJmqmcX4vXIheorq2Zhjdj27wAI3EkI)

The GitHub Actions runner permits loose conversions in `${{ }}` expressions. It converts an object to `'Object'`;
for example, `echo '${{ env }}'` becomes `echo 'Object'`. It also converts an array to `'Array'`. These conversions
are mistakes in almost all cases.
actionlint checks types more strictly and reports objects and arrays interpolated into strings.
The runner converts null to an empty string. To inspect an object or array, use `toJSON()`.

```sh
echo '${{ toJSON(github.event) }}'
```

actionlint uses two kinds of object types. A strict object reports a type error when an unknown property is accessed.
An open object permits unknown properties and gives them the type `any`.

When actionlint cannot determine a type statically, it uses `any`. This can happen when it cannot determine the value passed to `fromJSON()`.

An expression in `${{ }}` can also expand an object or array value.

Example input:

```yaml
on: push
jobs:
  test:
    strategy:
      matrix:
        env_string:
          - "FOO=BAR"
          - "FOO=PIYO"
        env_object:
          - FOO: BAR
          - FOO: PIYO
    runs-on: ubuntu-latest
    steps:
      # OK: Expanding object at 'env:' section
      - run: echo "$FOO"
        env: ${{ matrix.env_object }}
      # ERROR: String value cannot be expanded as object
      - run: echo "$FOO"
        env: ${{ matrix.env_string }}
permissions: {}
```

Output:

```console
test.yaml:19:14: env must be object but found string [expression]
   |
19 |         env: ${{ matrix.env_string }}
   |              ^~~
```

[Playground](https://kjanat.github.io/actionlint/#eNqckLFqhTAUhnef4ie4pg8Q6KCD0CnFrVNRCRqpieSclBbx3S+5KiJudwrnI/+fL8c7hTnSkI2+JZUBbIjTCRCHhk3/v03A1HCwf8cEGPf7TRys608GSIhK6/eyqMWdfn58aXEp8O1oOr4WVForlEV9hyn/pCE6ksk9ttFxlD9N8t61zUxHoUw3FUw3eIi80tfXFfJl2f/1dtpgXV+Lb9tI8dmEyRJZ70hhWR8DADTaX1g=)

In this example, the environment variable mapping is expanded in `env:`. actionlint checks the type of the expanded value.

Use `${{ insert }}` as an `env:` mapping key to insert additional variables beside
explicit entries. Its value must be a mapping or an expression producing an object:

```yaml
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo hello
        env:
          MODE: test
          ${{ insert }}: ${{ fromJSON(vars.ENVIRONMENT) }}
permissions: {}
```

actionlint checks the inserted value as part of the surrounding `env:` mapping.
It rejects known arrays and scalars, and checks environment names and expressions in literal mappings.
It accepts object expressions whose contents are unknown, but cannot establish checkout or executable-bit facts that depend on those contents. The
directive applies to workflow, job, step, container and service environment
mappings. `${{ insert }}` in an ordinary value is an undefined variable.

Mapping insertion also works in action `with:` inputs and at the matrix mapping:

```yaml
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        base: [one]
        ${{ insert }}: ${{ fromJSON('{"extra":[1,2]}') }}
    steps:
      - uses: example/action@v1
        with:
          path: source
          ${{ insert }}: ${{ fromJSON(vars.ACTION_INPUTS) }}
permissions: {}
```

actionlint checks known inserted action inputs for unknown names and missing required inputs.
It defers missing-input findings when an unknown mapping could supply those inputs.
Matrix and runner-label checks include known inserted axes and filters.
An unknown insertion can supply additional axes or values that actionlint cannot enumerate.

An expression-produced array inside a matrix axis or `include`/`exclude` sequence
splices one level into that sequence. A literal nested array counts as one
value. For example, `[before, "${{ fromJSON('[1,2]') }}", after]` has four
values, while `[before, [1,2], after]` has three.

`case(predicate, result, ..., default)` requires boolean predicates and evaluates only the selected result branch.
The result can be falsy, an object or an array. Use it to select an object for insertion.
`condition && value || fallback` short-circuits too, but chooses the fallback when `value` is falsy.

The result type of `case()` merges every reachable value branch and the final
default, excluding predicates. `case(github.ref == 'refs/heads/main',
'production', 'development')` has type `string`, so using it for
`timeout-minutes` reports a type error. Objects and arrays merge according to their property and element types.
Strings, numbers and booleans follow the scalar merging rules. Incompatible results fall back to `any`.
If a constant predicate makes a branch unreachable, that branch does not affect
the result type. For example, `case(true, 10, 'never')` has type `number`.
String literals are typed as `string`.

<a id="check-contexts-and-builtin-func"></a>

## Contexts and built-in functions

Example input:

```yaml
on: [push, issues]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      # Access undefined context
      - run: echo '${{ unknown_context }}'
      # Access undefined property of context
      - run: echo '${{ github.events }}'
      # Calling undefined function (start's'With is correct)
      - run: echo "${{ startWith('hello, world', 'lo,') }}"
      # Wrong number of arguments
      - run: echo "${{ startsWith('hello, world') }}"
      # An object argument makes this predicate return false.
      - run: echo "${{ startsWith('hello, world', github.event) }}"
      # contains() accepts either a string or an array as its first argument.
      - run: echo "${{ contains('hello, world', 'lo,') }}"
      - run: echo "${{ contains(github.event.issue.labels.*.name, 'enhancement') }}"
      # fromJSON() supplies an array of event names to contains().
      - if: contains(fromJSON('["push", "pull_request"]'), github.event_name)
        run: echo 'Push or pull request'
permissions: {}
```

Output:

```console
test.yaml:7:24: unknown-variable: undefined variable "unknown_context". available variables are "env", "github", "inputs", "job", "matrix", "needs", "runner", "secrets", "steps", "strategy", "vars" [expression-reference]
  |
7 |       - run: echo '${{ unknown_context }}'
  |                        ^~~~~~~~~~~~~~~
test.yaml:9:24: unknown-property: property "events" is not defined in github; did you mean "event"? [expression-reference]
  |
9 |       - run: echo '${{ github.events }}'
  |                        ^~~~~~~~~~~~~
test.yaml:11:24: unknown-function: undefined function "startWith"; did you mean "startsWith"? [expression-reference]
   |
11 |       - run: echo "${{ startWith('hello, world', 'lo,') }}"
   |                        ^~~~~~~~~
test.yaml:13:24: argument-count: number of arguments is wrong. function "startsWith(string, string) -> bool" takes 2 parameters but 1 arguments are given [expression-call]
   |
13 |       - run: echo "${{ startsWith('hello, world') }}"
   |                        ^~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNqckUFO6zAQhvc9xch6ktsnJwfIEVgAEgsWVRU5YYoNzjh4xhQpyt2RSwUtol10lcV83z8T/5EaWI+ZnQHPnJE3i5fYcbMAEGQpX4CUiatC5i6T5CrYMtuPWHDkLwqgKmQD2LsI+t80QaZXijtq+0iCHwLzrM+hz15c7mp8RxI+A6oCstgkj17cUjsMIRrYxRSetAEdotErmGd10eU/5Ossc3L1xYzyAtYTX3H0t3q8rN63VQfbYeD6f012QAMayVnqcUCSX6F+2/wkbVMcbh7ubpd6rUr5yoAacwhtwreMLGqjV6c/15YFq0MaHJd3n9lBTFB8OPh6MWIaPLOPxA1M8+cAkFHBYQ==)

[Contexts][contexts-doc] and [built-in functions][funcs-doc] are strongly typed. actionlint reports misspelled context
properties and function names, as well as invalid calls with the wrong number or types of arguments.

The checker handles these function signatures:

- some functions are overloaded (e.g. `contains(str, substr)` and `contains(array, item)`)
- some parameters are optional (e.g. `join(strings, sep)` and `join(strings)`)
- some parameters are repeatable (e.g. `hashFiles(file1, file2, ...)`)

Context and function names are case-insensitive. For example, `toJSON` and `toJson` are the same function.

actionlint also checks the contents of arguments to these functions:

- `format()`: Checks placeholders in the format string passed as the first argument.
- `fromJSON()`: Checks the JSON string and infers the result type from its contents.

Example input:

```yaml
on: [push, issues]

jobs:
  test:
    # ERROR: Key 'mac' does not exist in the object returned by the fromJSON()
    runs-on: ${{ fromJSON('{"win":"windows-latest","linux":"ubuntul-latest"}')['mac'] }}
    steps:
      # ERROR: {2} is missing in the first argument of format()
      - run: echo "${{ format('{0}{1}', 1, 2, 3) }}"
      # ERROR: Argument for {2} is missing in the arguments of format()
      - run: echo "${{ format('{0}{1}{2}', 1, 2) }}"
      - run: echo This is a special branch!
        # ERROR: Broken JSON string. Special check for fromJSON()
        if: contains(fromJson('["main","release","dev"'), github.ref_name)
permissions: {}
```

Output:

```console
test.yaml:6:18: unknown-property: property "mac" is not defined in this object [expression-reference]
  |
6 |     runs-on: ${{ fromJSON('{"win":"windows-latest","linux":"ubuntul-latest"}')['mac'] }}
  |                  ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:9:24: format-placeholder: format string "{0}{1}" does not contain placeholder {2}. remove argument which is unused in the format string [expression-call]
  |
9 |       - run: echo "${{ format('{0}{1}', 1, 2, 3) }}"
  |                        ^~~~~~
test.yaml:9:24: unknown-script-value: interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution [script-interpolation]
  |
9 |       - run: echo "${{ format('{0}{1}', 1, 2, 3) }}"
  |                        ^~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:11:24: format-placeholder: format string "{0}{1}{2}" contains placeholder {2} but only 2 arguments are given to format [expression-call]
   |
11 |       - run: echo "${{ format('{0}{1}{2}', 1, 2) }}"
   |                        ^~~~~~
test.yaml:14:31: invalid-json: broken JSON string is passed to fromJSON() at offset 23: expected ',' or ']' after JSON array value [expression-call]
   |
14 |         if: contains(fromJson('["main","release","dev"'), github.ref_name)
   |                               ^~~~~~~~~~~~~~~~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNqMj8FKAzEQhu/7FL+DkF1IxdZbHsGDHvRWimS3qRvZnSyZxAoh7y5b7VHwMnOY/5uPP7DBfskyaniR7OTQNB+hF9MAyUlaNxAzy2aN3paCUwzz48vzU6sKnT2TWecxnGUz2RUhTZPn/EWGcp855el6qKrbq9kO6oBaL48luUV+HMBm9Ri4YQygiynE2aZWlftatlVpbDV2Gg8daqX/QWV35f6CXkcv8AILWdzg7YQ+Wh7Gm98s4E8GQ+BkPUt7aS+BW7Wn2XomTdFNzoojTUf3SarTePdpzP1ddKc3trPrmsXF2Yv4wGJQ6vcAwFpyyA==)

GitHub Actions has no syntax for array or object literals. Use `fromJSON()` to create them.

<a id="check-contextual-step-object"></a>

## Contextual typing for `steps.<step_id>` objects

Example input:

```yaml
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    outputs:
      # Step outputs can be used in job outputs since this section is evaluated after all steps were run
      foo: "${{ steps.get_value.outputs.name }}"
    steps:
      # ERROR: Access undefined step outputs
      - run: echo '${{ steps.get_value.outputs.name }}'
      # Outputs are set here
      - run: echo "foo=value" >> "$GITHUB_OUTPUT"
        id: get_value
      # OK
      - run: echo '${{ steps.get_value.outputs.name }}'
      # OK
      - run: echo '${{ steps.get_value.conclusion }}'
  other:
    runs-on: ubuntu-latest
    steps:
      # ERROR: Access undefined step outputs. Step objects are job-local
      - run: echo '${{ steps.get_value.outputs.name }}'
permissions: {}
```

Output:

```console
test.yaml:10:24: unknown-property: property "get_value" is not defined in steps [expression-reference]
   |
10 |       - run: echo '${{ steps.get_value.outputs.name }}'
   |                        ^~~~~~~~~~~~~~~
test.yaml:15:24: unknown-script-value: interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution [script-interpolation]
   |
15 |       - run: echo '${{ steps.get_value.outputs.name }}'
   |                        ^~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:22:24: unknown-property: property "get_value" is not defined in steps [expression-reference]
   |
22 |       - run: echo '${{ steps.get_value.outputs.name }}'
   |                        ^~~~~~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNqskDFLxEAQhfv7FY/l4KrkByx4hY1aaZHURy5OTCTZWTIzNiH/XXaNVgEFr5rifd88eBw8okl/eOer+AOgJJouMFuQIuV2taBWjE3KcsSm0VS+OKBj9nDHZYEoRSnfSC8fzWhUbmAZmomwri4LGfp2i9TjQW3POP3hxWnHcx3zXaYdzme448NT9VjfX57r6qWu3GYAw6vHz+Mb9u96LYd2NBk4bBZrT/Ov0/5znEjzNEhqFY9l/RwAbpWYcw==)

Access step outputs through `steps.<step_id>`. The `steps` context is dynamic:

- Accessing outputs before the step runs returns `null`.
- Only outputs of steps in the same job are accessible.

Copying a step without updating its references can cause it to access the wrong outputs.
actionlint reports invalid references to step outputs.

When actionlint has a valid composite action manifest, its output mapping determines the available output names.
It checks those names in workflows, nested composite steps, and composite output values.
actionlint accepts references to undeclared outputs of JavaScript and Docker actions because they can create outputs at runtime.
Without metadata, actionlint cannot determine the declared output names.
In the JavaScript example below, actionlint accepts `cache_hit` and checks step order and unsafe script interpolation.

Example input:

```yaml
on: push

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      # ERROR: The step is not run yet at this point
      - run: echo ${{ steps.cache.outputs.cache-hit }}
      # actions/cache sets cache-hit output
      - uses: actions/cache@v6
        id: cache
        with:
          key: ${{ hashFiles('**/*.lock') }}
          path: ./packages
      # The output is an unknown string in this script
      - run: echo ${{ steps.cache.outputs.cache-hit }}
      # A metadata provider can also detect this misspelled output name
      - run: echo ${{ steps.cache.outputs.cache_hit }}
permissions: {}
```

Output:

```console
test.yaml:8:23: unknown-property: property "cache" is not defined in steps [expression-reference]
  |
8 |       - run: echo ${{ steps.cache.outputs.cache-hit }}
  |                       ^~~~~~~~~~~
test.yaml:16:23: unknown-script-value: interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution [script-interpolation]
   |
16 |       - run: echo ${{ steps.cache.outputs.cache-hit }}
   |                       ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:18:23: unknown-script-value: interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution [script-interpolation]
   |
18 |       - run: echo ${{ steps.cache.outputs.cache_hit }}
   |                       ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNqkj8FKxTAQRff9irsQnhba7lzMypW/IXlxcGJrE97MKFL675K21K1gNmHmnMtN8kwortI07/mq1ADGavUGbj5rVwW/+mzeTaGyDalx0d0CumoSOErG3bLssI8hCvfZrbgdUyfJsK5nzJWVEKKlPOuwKU+fjwcG0ithW56br2RC5wSM/E1bpQSV5zSx3l/admj7Kcfx8vDbVU8JJoR+KCGO4Y31v6//Y+zliBW+fSTV+lXCsv4MAIvrcvY=)

In the above example, [actions/cache][actions-cache] action sets `cache-hit` output so that the following steps can know
whether the cache was hit or not. At line 8, the cache action is not run yet. So `cache` property does not exist in the
`steps` context yet. On running the step whose ID is `cache`, `steps.cache` object is typed as
`{outputs: {cache-hit: any}, conclusion: string, outcome: string}`. At line 18, the expression has a typo in the output
name. actionlint can check it because properties of `steps.cache.outputs` are typed.

This strict output typing also applies to local actions. Consider the following action.

```yaml
name: "My action with output"
author: "rhysd <https://rhysd.github.io>"
description: "my action with outputs"

outputs:
  some_value:
    description: some value returned from this action

runs:
  using: "node24"
  main: "index.js"
```

Example input:

```yaml
on: push

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      # ERROR: The step is not yet run
      - run: echo ${{ steps.my_action.outputs.some_value }}
      # The action runs here and sets its outputs
      - uses: ./.github/actions/my-action-with-output
        id: my_action
      # OK
      - run: echo ${{ steps.my_action.outputs.some_value }}
      # ERROR: No output named 'some-value' (typo)
      - run: echo ${{ steps.my_action.outputs.some-value }}
permissions: {}
```

Output:

<!-- Skip update output -->

```console
test.yaml:8:23: property "my_action" is not defined in object type {} [expression]
  |
8 |       - run: echo ${{ steps.my_action.outputs.some_value }}
  |                       ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:15:23: property "some-value" is not defined in object type {some_value: string} [expression]
   |
15 |       - run: echo ${{ steps.my_action.outputs.some-value }}
   |                       ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
```

<!-- Skip playground link -->

The 'My action with output' action defines `some_value`. actionlint types that property in `steps.my_action.outputs`,
so it can report incorrect accesses such as a misspelled output name.

<a id="check-contextual-matrix-object"></a>

## Contextual typing for `matrix` object

Example input:

```yaml
on: push
jobs:
  test:
    strategy:
      matrix:
        os: [ubuntu-latest, windows-latest]
        node: [14, 15]
        package:
          - name: "foo"
            optional: true
          - name: "bar"
            optional: false
        include:
          - node: 15
            npm: 7.5.4
    runs-on: ${{ matrix.os }}
    steps:
      # Access undefined matrix value
      - run: echo '${{ matrix.platform }}'
      # Matrix value is strongly typed. Below line causes an error since matrix.package is {name: string, optional: bool}
      - run: echo '${{ matrix.package.dev }}'
      # OK
      - run: |
          echo 'os: ${{ matrix.os }}'
          echo 'node version: ${{ matrix.node }}'
          echo 'package: ${{ matrix.package.name }} (optional=${{ matrix.package.optional }})'
      # Additional matrix values in 'include:' are supported
      - run: echo 'npm version is specified'
        if: ${{ contains(matrix.npm, '7.5') }}
  test2:
    runs-on: ubuntu-latest
    steps:
      # Matrix values in other job is not accessible
      - run: echo '${{ matrix.os }}'
permissions: {}
```

Output:

```console
test.yaml:19:24: unknown-property: property "platform" is not defined in matrix [expression-reference]
   |
19 |       - run: echo '${{ matrix.platform }}'
   |                        ^~~~~~~~~~~~~~~
test.yaml:21:24: unknown-property: property "dev" is not defined in matrix.package [expression-reference]
   |
21 |       - run: echo '${{ matrix.package.dev }}'
   |                        ^~~~~~~~~~~~~~~~~~
test.yaml:24:25: unknown-script-value: interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution [script-interpolation]
   |
24 |           echo 'os: ${{ matrix.os }}'
   |                         ^~~~~~~~~
test.yaml:25:35: unknown-script-value: interpolated scalar is not proved to occupy a script data argument; pass it through an environment variable and read that variable as data instead [script-interpolation]
   |
25 |           echo 'node version: ${{ matrix.node }}'
   |                                   ^~~~~~~~~~~
test.yaml:26:30: unknown-script-value: interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution [script-interpolation]
   |
26 |           echo 'package: ${{ matrix.package.name }} (optional=${{ matrix.package.optional }})'
   |                              ^~~~~~~~~~~~~~~~~~~
test.yaml:26:67: unknown-script-value: interpolated scalar is not proved to occupy a script data argument; pass it through an environment variable and read that variable as data instead [script-interpolation]
   |
26 |           echo 'package: ${{ matrix.package.name }} (optional=${{ matrix.package.optional }})'
   |                                                                   ^~~~~~~~~~~~~~~~~~~~~~~
test.yaml:34:24: unknown-property: property "os" is not defined in matrix [expression-reference]
   |
34 |       - run: echo '${{ matrix.os }}'
   |                        ^~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNqMUu1q4zAQ/J+nGMKBE4gNOWICgnuScD8UW07U2pLQSklLqncviq06Hy7tL7GrmdXsjLRiMJ6Osxe9JzYDnCAXT4Cc5U4c3vsK6Liz8i1VgCaGnd975Xze8shb4SxVrc801P+/sErXgmG33qywLse24dUrP4hxJpBD8U4wzBut5zd9QBsnteItg7NeTFH23H5HaXhLI0eqqvX147tXjevyboIyHcO2KIvNtW29ojx69udyGQwpNCGEwTFhKA3NI5hBVEeN7AZuWu4abTuEkP0E7e0panF6Rn/c6OyZMZBHXdkTKq6Jk7AkH/a4XkwxUkqYkBadRwhYJKv/TYDSHUJYTu6sTJckQRLIiEo2UtSjFtn0z1daOS4VLZJo062QbYsyW/YpxH/3l92HdfdJf5/UYKARtpMUtRHDJXwOAPu56uI=)

actionlint infers the type of each matrix axis from the elements in its array.
For an array of objects, it checks property names and types, such as `package.name` in the example above.

When actionlint cannot infer a common type for the array elements, it uses `any` for the matrix value.

```yaml
strategy:
  matrix:
    foo:
      - "string value"
      - 42
      - { aaa: true, bbb: null }
    bar:
      - [42]
      - [true]
      - [{ aaa: true, bbb: null }]
      - []
steps:
  # matrix.foo is any type value
  - run: echo ${{ matrix.foo }}
  # matrix.bar is array<any> type value
  - run: echo ${{ matrix.bar[0] }}
  # ERROR: Array cannot be evaluated as string
  - run: echo ${{ matrix.bar }}
```

The type of a scalar matrix value follows how GitHub resolves the scalar. A quoted scalar, a scalar
with an explicit `!!str` tag, and a block scalar are always `string`, even when their text looks like
a number or a boolean.

A plain scalar is resolved with the YAML 1.2 core schema. `true`, `True`, `TRUE` and their `false`
counterparts are `bool`. An empty value, `null`, `Null`, `NULL` and `~` are `null`. A decimal integer
with an optional sign, a `0x` hexadecimal, a `0o` octal, a decimal fraction, an exponent form, and
`.inf` and `.nan` with their case variants are `number`. Everything else is `string`, including
`ubuntu-latest`, `yes`, `on`, a date such as `2026-08-21`, and numeric spellings the core schema does
not have, such as `0b10`, `-0x10` and `1_000`.

```yaml
strategy:
  matrix:
    version:
      # string values
      - "3.10"
      - !!str 3.11
      - >-
        3.12
      # also string: the core schema has no binary integer
      - 0b10
    flag:
      # bool values
      - true
      - True
    size:
      # number values
      - 0x1F
      - 0o17
      - .inf
```

<a id="check-matrix-value-tags"></a>

## YAML tags of matrix values

Example input:

```yaml
on: push
jobs:
  test:
    strategy:
      matrix:
        version:
          # ERROR: The core schema has no binary integer, so "!!int" does not accept this value
          - !!int 0b10
        flag:
          # ERROR: A quoted scalar cannot have a tag other than "!!str"
          - !!bool "true"
        released:
          # ERROR: A matrix value cannot have a tag outside the core schema
          - !!timestamp 2026-08-21
    runs-on: ubuntu-latest
    steps:
      - run: echo '${{ matrix.version }} ${{ matrix.flag }} ${{ matrix.released }}'
permissions: {}
```

Output:

```console
test.yaml:8:13: invalid value "0b10" for "!!int" tag [syntax-check]
  |
8 |           - !!int 0b10
  |             ^~~~~
test.yaml:11:13: tag of a quoted or block scalar must be "!!str" but got "!!bool" [syntax-check]
   |
11 |           - !!bool "true"
   |             ^~~~~~
test.yaml:14:13: tag of a YAML scalar must be one of "!!str", "!!bool", "!!int", "!!float", "!!null" but got "!!timestamp" [syntax-check]
   |
14 |           - !!timestamp 2026-08-21
   |             ^~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNpcj81OxCAUhfd9ijMTk1lhmC6M4W1ArzMY/sK9GE3Duxu0raYrcg4f5Ds5GZTG9+k9OzYTIMQyToClWqHb128CopXqP7cEfFBln9NfASicTj4JtLvqvX4L9naEXM4BZ6mNzvtNpUCW6fXIio/EYmPBrOcnpZ/VfP1BakusxoDmWpKmgh3yqzsV3j5SgzSgl3vG5WFZ1iWP6wD0jn/t0D1Umxl6v0yFavQ8HrLB0r8HAGKgWqY=)

GitHub reads a matrix scalar with the YAML 1.2 core schema and rejects a workflow whose explicit tag does not fit the
scalar. A tag other than `!!str` is allowed only on a plain scalar, and `!!bool`, `!!int`, `!!float` and `!!null` accept
only the values their core schema production matches. Any other tag is rejected.

actionlint reports such a tag while parsing and types the value as `string`.

<a id="check-contextual-needs-object"></a>

## Contextual typing for `needs` object

Example input:

```yaml
on: push
jobs:
  install:
    outputs:
      installed: "..."
    runs-on: ubuntu-latest
    steps:
      - run: echo 'install something'
  prepare:
    outputs:
      prepared: "..."
    runs-on: ubuntu-latest
    steps:
      - run: echo 'parepare something'
      # ERROR: Outputs in other job is not accessible
      - run: echo '${{ needs.prepare.outputs.prepared }}'
  build:
    needs: [install, prepare]
    outputs:
      built: "..."
    runs-on: ubuntu-latest
    steps:
      # OK: Accessing job results
      - run: echo 'build something with ${{ needs.install.outputs.installed }} and ${{ needs.prepare.outputs.prepared }}'
      # ERROR: Accessing undefined output causes an error
      - run: echo '${{ needs.install.outputs.foo }}'
      # ERROR: Accessing undefined job ID
      - run: echo '${{ needs.some_job }}'
  other:
    runs-on: ubuntu-latest
    steps:
      # ERROR: Cannot access outputs across jobs
      - run: echo '${{ needs.build.outputs.built }}'
permissions: {}
```

Output:

```console
test.yaml:16:24: unknown-property: property "prepare" is not defined in needs [expression-reference]
   |
16 |       - run: echo '${{ needs.prepare.outputs.prepared }}'
   |                        ^~~~~~~~~~~~~
test.yaml:24:45: unknown-script-value: interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution [script-interpolation]
   |
24 |       - run: echo 'build something with ${{ needs.install.outputs.installed }} and ${{ needs.prepare.outputs.prepared }}'
   |                                             ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:24:88: unknown-script-value: interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution [script-interpolation]
   |
24 |       - run: echo 'build something with ${{ needs.install.outputs.installed }} and ${{ needs.prepare.outputs.prepared }}'
   |                                                                                        ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:26:24: unknown-property: property "foo" is not defined in needs.install.outputs [expression-reference]
   |
26 |       - run: echo '${{ needs.install.outputs.foo }}'
   |                        ^~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:28:24: unknown-property: property "some_job" is not defined in needs [expression-reference]
   |
28 |       - run: echo '${{ needs.some_job }}'
   |                        ^~~~~~~~~~~~~~
test.yaml:33:24: unknown-property: property "build" is not defined in needs [expression-reference]
   |
33 |       - run: echo '${{ needs.build.outputs.built }}'
   |                        ^~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNqkkdFq8zAMhe/zFIfyQ2/+5AH8KmOMZNEWl9Qylswugt992HUdtpVtrBeBKJLOd47CzsBHWboTT2I6wDrRcV3zK8BRfVS5FK1Hs8FhGIZD+Ryikz7LxCk6jf06KomWlij5ttznSQN6XhjHqgThM+li3euxA3wgPwa6ia69+8lZJT8f0V/G/m0bHNEsQyUP1dC1npFS3pyiXecLqcwbPNRw/6+mH28Fynt6d5pC36PgzeqC3Xt10ry3H4iUMLoZv475zYE+Q16Yf17Klp9OPNVJ1oWC+fsdduFykeYlV1oYnsLZilh2YrCl9wEAcCTxDw==)

Define job dependencies with [`needs:`][needs-doc]. A job runs after all jobs in its `needs:` list finish.
The [`needs` context][needs-context-doc] gives downstream jobs access to those jobs' outputs.

actionlint determines the type of `needs` from each job's `outputs:` and `needs:` sections.

<a id="check-comparison-types"></a>

## Comparison coercions

GitHub Actions accepts comparisons between different types and converts their values
according to [its expression rules][operators-doc]. actionlint accepts these comparisons
and infers a boolean result. For example, `true > false` evaluates to `true`, and
`fromJSON('[]') == 0` evaluates to `false` because an array converts to `NaN`.

String parameters also accept scalar conversions: `contains(null, '')` and
`startsWith(true, 'TR')` both evaluate to `true`. Their inferred return type is boolean.

To warn about boolean or null string arguments and surprising comparison operands,
enable `lint.rules.suspicious.coercion: warn`. The rule is off by default.
It reports a possible mistake, so it offers no automatic fix. The separate
`mixed-type-comparisons` rule checks string-versus-number or boolean conditions.

<a id="check-executable-bit"></a>

## Executable bits on repository scripts

Example input:

```yaml
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - run: ./bad.sh # ERROR: tracked with Git mode 100644
permissions: { contents: read }
```

Output:

<!-- Skip update output -->

```console
test.yaml:7:14: script "bad.sh" is executed directly but its Git index mode is 100644 (not executable); commit an executable bit with git update-index --chmod=+x, or invoke its interpreter explicitly [executable-bit]
```

<!-- Skip playground link -->

This example requires `bad.sh` in the repository's Git index with mode `100644`.
The `executable-bit` rule reads index modes, so it also works when actionlint runs
on Windows and checks workflows targeting Linux or macOS. It requires Git and a
detected local repository, but does not require ShellCheck. Missing Git, untracked
files, unresolved merge entries, symlinks and submodules do not produce findings.

Either commit the executable bit with `git update-index --chmod=+x bad.sh`, run
`bash bad.sh` instead, or set permissions in the workflow before invoking the
script. A literal `chmod` of an indexed regular file in an earlier step or command
invalidates the index mode for that path. Missing or untracked operands leave
subsequent execution uncertain. See GitHub's [Adding scripts to your workflow][workflow-scripts-doc].

The rule shares the [run-directory resolver](config.md#shellcheck). It follows
step, job and workflow working-directory defaults, and recognizes a literal `cd`
within a step. The next step starts from its own configured working directory.
A self checkout establishes the repository location; a static checkout `path`
is supported, including calls such as `./source/bad.sh` from the workspace root.

Detection is deliberately conservative. It checks direct literal calls in plain
Bash or sh steps on recognized GitHub-hosted Linux/macOS runners after a self checkout.
Literal script operands to `exec` and `command` are also checked, with an optional
`--` separator; other options and lookup-only forms such as `command -v` are skipped.
Repository settings on self-hosted and grouped runners can come from previous jobs.
Reusable workflows may check out a different caller repository, so their local
index modes are not assumed to match the runner. Steps that can run after failure,
such as `if: always()`, cannot assume checkout completed successfully.
Alternate checkout refs/repositories, containers, dynamic expressions, custom shells,
shell startup environment settings, parallel/background steps and unsupported
shell control flow are skipped. Earlier opaque actions or commands may change
permissions, replace files or change Git settings, so subsequent calls in that
job are skipped. A later checkout cannot guarantee that working-tree modes match
the index when `core.fileMode` has changed. Interpreter and source calls do not require an executable
bit on their argument. Shell syntax is parsed without executing any script.

Referenced local composite actions are checked too, including nested calls.
Their scripts use the caller's checkout and permission state, but their own
shell and working directory. Findings point to `action.yml` or `action.yaml`.
Remote action contents are not downloaded for these checks.

[workflow-scripts-doc]: https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/add-scripts
<a id="check-shellcheck-integ"></a>

## [shellcheck][shellcheck] integration for `run:`

Example input:

```yaml
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo $FOO
  test-win:
    runs-on: windows-latest
    steps:
      # Shell on Windows is PowerShell by default.
      # shellcheck is not run in this case.
      - run: echo $FOO
      # This script is run with bash due to 'shell:' configuration
      - run: echo $FOO
        shell: bash
permissions: {}
```

Output:

```console
test.yaml:6:19: SC2086: Double quote to prevent globbing and word splitting [shellcheck]
  |
6 |       - run: echo $FOO
  |                   ^~~~
test.yaml:14:19: SC2086: Double quote to prevent globbing and word splitting [shellcheck]
   |
14 |       - run: echo $FOO
   |                   ^~~~
```

<!-- Skip playground link -->

[shellcheck][shellcheck] is a famous linter for ShellScript. actionlint runs shellcheck for scripts at `run:` step in a workflow.
For installation instructions, see [the ShellCheck documentation][shellcheck-install].

actionlint determines the script's shell according to [GitHub's shell selection rules][shell-doc].
The default is `bash` on Linux and macOS, and `pwsh` on Windows.
Set `shell:` on a step or under the job or workflow defaults to choose a shell.

In the above example output, `SC2086:info:1:6:` means that shellcheck reported SC2086 rule violation and the location is at
line 1, column 6 relative to the script of the `run:` section. When that script position can be mapped exactly to the YAML
source, actionlint reports and underlines the offending range there. This mapping is supported for plain scalars and literal
blocks. Other scalar forms, such as folded blocks, fall back to reporting the `run:` key when an exact mapping is not safe.

actionlint applies ShellCheck to recognized `bash`, `sh`, `dash`, and `ksh` interpreters, including executable paths and
supported custom shell templates. A leading `# shellcheck shell=...` directive can select a supported dialect explicitly.

By default, actionlint checks if `shellcheck` command exists in your system and uses it when it is found. The `--tool`
option takes a command line after the tool id: a command name, a file path, or a command with flags such as
`--tool shellcheck='shellcheck -e SC2086'`. Those arguments are prepended to the ones actionlint appends itself, so
`-f`/`--format` and file arguments must not be passed. `--no-tool shellcheck` disables shellcheck integration explicitly.

By default, actionlint passes `--norc` to disable rc-file discovery. Set
[`tools.shellcheck.config`](config.md#shellcheck) to inline settings, an rc-file path, or a directory containing
`.shellcheckrc` or `shellcheckrc`. Selecting an rc file replaces `--norc` with `--rcfile`; relative config paths resolve from
the selected actionlint configuration file's directory. Additional options can use `--tool shellcheck='<command line>'` or the
`SHELLCHECK_OPTS` environment variable described below.

ShellCheck parses `${{ }}` placeholders as shell syntax, which can cause false positives.
Before calling ShellCheck, actionlint replaces each placeholder with underscores.
For example, `echo '${{ matrix.os }}'` becomes `echo '________________'`.

actionlint disables [SC2050][SC2050], [SC2194][SC2194], [SC2157][SC2157] and [SC2043][SC2043] because expression
placeholders can trigger them. It also disables [SC1091][SC1091] because sourced files may exist only on the runner,
and [SC2153][SC2153] and [SC2154][SC2154] because workflow environment variables can be supplied externally.
These exclusions still apply when optional checks are enabled through configuration, directives or command-line options.

When an explicit shell expression cannot be resolved, or a custom wrapper's script language cannot be inferred, actionlint
skips ShellCheck unless a leading native `shell` directive selects a supported dialect. Global dialect settings and command
flags alone do not select unknown or non-shell scripts. If no shell is configured and the runner's default shell is unknown,
actionlint leaves the shell unknown. Set `shell:` explicitly to identify the interpreter, for example for a step guarded by a
matrix value:

```yaml
- name: Show file content
  run: Get-Content -Path xxx\yyy.txt
  if: ${{ matrix.os == 'windows-latest' }}
  shell: pwsh
```

Use the [`SHELLCHECK_OPTS` environment variable][shellcheck-env-var] to configure ShellCheck.

From the command line:

```sh
# Enable some optional rules
SHELLCHECK_OPTS='--enable=avoid-nullary-conditions' actionlint

# Disable some rules
SHELLCHECK_OPTS='--exclude=SC2129' actionlint
```

On GitHub Actions:

```yaml
- run: actionlint
  env:
    SHELLCHECK_OPTS: --exclude=SC2129
```

<a id="untrusted-inputs"></a>

## Script injection by potentially untrusted inputs

Example input:

```yaml
name: Test
on: [pull_request, push]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Print pull request title
        # ERROR: Using the potentially untrusted input can cause script injection
        run: echo '${{ github.event.pull_request.title }}'
      - uses: actions/stale@v10
        with:
          repo-token: ${{ secrets.TOKEN }}
          # This is OK because action input is not evaluated by shell
          stale-pr-message: ${{ github.event.pull_request.title }} was closed
      - uses: actions/github-script@v9
        with:
          # ERROR: Using the potentially untrusted input can cause script injection
          script: console.log('${{ github.event.head_commit.author.name }}')
      - name: Get comments
        # ERROR: Accessing untrusted inputs via `.*` object filter; bodies of comment, review, and review_comment
        run: echo '${{ toJSON(github.event.*.body) }}'
      - name: Do something with checking skip
        # OK: This placeholder uses an untrusted input, but the input cannot be injected to the script
        run: if [ "${{ contains(github.event.pull_request.title, '[SKIP]') }}" = "true" ]; then echo "skip"; fi
permissions: {}
```

Output:

```console
test.yaml:10:24: untrusted-interpolation: "github.event.pull_request.title" is potentially untrusted. avoid using it directly in inline scripts. instead, pass it through an environment variable. see https://docs.github.com/en/actions/reference/security/secure-use#good-practices-for-mitigating-script-injection-attacks for more details [script-injection]
   |
10 |         run: echo '${{ github.event.pull_request.title }}'
   |                        ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:19:36: untrusted-interpolation: "github.event.head_commit.author.name" is potentially untrusted. avoid using it directly in inline scripts. instead, pass it through an environment variable. see https://docs.github.com/en/actions/reference/security/secure-use#good-practices-for-mitigating-script-injection-attacks for more details [script-injection]
   |
19 |           script: console.log('${{ github.event.head_commit.author.name }}')
   |                                    ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:22:31: untrusted-interpolation: object filter extracts potentially untrusted properties "github.event.comment.body", "github.event.discussion.body", "github.event.issue.body", "github.event.pull_request.body", "github.event.review.body", "github.event.review_comment.body". avoid using the value directly in inline scripts. instead, pass the value through an environment variable. see https://docs.github.com/en/actions/reference/security/secure-use#good-practices-for-mitigating-script-injection-attacks for more details [script-injection]
   |
22 |         run: echo '${{ toJSON(github.event.*.body) }}'
   |                               ^~~~~~~~~~~~~~~~~~~
test.yaml:25:24: unknown-script-value: interpolated scalar is not proved to occupy a script data argument; pass it through an environment variable and read that variable as data instead [script-interpolation]
   |
25 |         run: if [ "${{ contains(github.event.pull_request.title, '[SKIP]') }}" = "true" ]; then echo "skip"; fi
   |                        ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNqMkUFr20AQhe/6FQ9RsFMitT12QyGHltIGkkByMybI64l3a2lH3Zl1KEb/vawUjN1i2tMyzJv3Pd6GpiODRxItOBgs+tS2T5F+JhK9RJ/ELYviB6/EFICSaH6BmIJU+SCtUtBUtU3ejStR6mVSARUmwH30QZHN8WoO9drSq2w0NCDrGLM3+z02Xl1a1bSjoPVxpno8wzDMDoQkJAaNVc9B3ok2LV3vPrw/WL94deYwAZF6rpS3FAwyS8hGUqkf726+3GIYjqSjWdXHqiORZkMG/xcOL43Atiy0PhNz8qjERt/r9e7j2bSTwsByEG6pbnkz/7siR836yXLXea2bpI5jnYvPPV388RVfSZGVFFTO1a/8/eHudn6CeFuveP3r4qT6yfEzQ7gjdT5sxvywjuw2T7L1/SnEP2OBMkMsB218kPk/+rzEbPFw8+1+OcvwEp9QakxUYnkFdRSm3GVGlVd49kVPsfMiuWeD/fB7ACq26os=)

GitHub Actions substitutes `${{ }}` values into `run:` scripts before the shell parses them.
An untrusted value can therefore become shell code. For example:

```yaml
- run: echo 'issue ${{github.event.issue.title}}'
```

an attacker can create a new issue with the title `'; malicious_command ...`, and the inline script will run
`echo 'issue'; malicious_command ...` in your workflow. Pass potentially untrusted values through environment variables
to prevent them from becoming shell code. See [GitHub's guidance][security-doc].

```yaml
- run: echo "issue ${TITLE}"
  env:
    TITLE: ${{github.event.issue.title}}
```

actionlint recognizes the following inputs as potentially untrusted and checks your inline scripts at `run:`. When they are used
directly in a script, actionlint will report it as an error.

- `github.event.issue.title`
- `github.event.issue.body`
- `github.event.pull_request.title`
- `github.event.pull_request.body`
- `github.event.comment.body`
- `github.event.review.body`
- `github.event.review_comment.body`
- `github.event.pages.*.page_name`
- `github.event.commits.*.message`
- `github.event.head_commit.message`
- `github.event.head_commit.author.email`
- `github.event.head_commit.author.name`
- `github.event.commits.*.author.email`
- `github.event.commits.*.author.name`
- `github.event.pull_request.head.ref`
- `github.event.pull_request.head.label`
- `github.event.pull_request.head.repo.default_branch`
- `github.head_ref`

actionlint also detects untrusted properties accessed through [object filters][object-filter-syntax].
For example, `github.event.*.body` collects the `body` properties of child objects into an array.
These can include `github.event.comment.body` and `github.event.pull_request.body`.

```sh
# Echo list of github.event.comment.body, github.event.pull_request.body, ...
echo '${{ toJSON(github.event.*.body) }}'
```

Pass the JSON string through an environment variable:

```sh
- run: echo "${BODIES}"
  env:
    BODIES: '${{ toJSON(github.event.*.body) }}'
```

The following functions return boolean values, so their results cannot inject script text. actionlint does not report
untrusted inputs passed to these functions.

- `contains()`
- `startswith()`
- `endswith()`

The [actions/github-script][github-script] action has the same risk in its `script` input. actionlint checks that input too.

<a id="check-job-deps"></a>

## Job dependencies validation

Example input:

```yaml
on: push
jobs:
  prepare:
    needs: [build]
    runs-on: ubuntu-latest
    steps:
      - run: echo 'prepare'
  install:
    needs: [prepare]
    runs-on: ubuntu-latest
    steps:
      - run: echo 'install'
  build:
    needs: [install]
    runs-on: ubuntu-latest
    steps:
      - run: echo 'build'
permissions: {}
```

Output:

```console
test.yaml:3:3: cyclic dependencies in "needs" job configurations are detected. detected cycle is "prepare" -> "build" -> "install" -> "prepare" [job-needs]
  |
3 |   prepare:
  |   ^~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNqkjjGuAyEMRHtOMd1WewGu8vULyFpaImIQg6sod48gNFtvZ/l53rioRzWe7lkivQNqkxqajBFQkYMef9FSPv7nqplyHymLpt32HLqwT8Qulb8gsI9LD3mcBduSbg5Iyh5yvvoXv9GwtKNhPnv1L3rDP6Wbq9JeiUxF6fH+fAcAsEhh/A==)

Define job dependencies with [`needs:`][needs-doc]. Jobs with cyclic dependencies cannot start. actionlint reports cycles,
undefined jobs, and duplicate jobs in `needs:`.

Example input:

```yaml
on: push
jobs:
  foo:
    needs: [bar, BAR]
    runs-on: ubuntu-latest
    steps:
      - run: echo 'hi'
  bar:
    needs: [unknown]
    runs-on: ubuntu-latest
    steps:
      - run: echo 'hi'
permissions: {}
```

Output:

```console
test.yaml:4:18: job ID "BAR" duplicates in "needs" section. note that job ID is case insensitive [job-needs]
  |
4 |     needs: [bar, BAR]
  |                  ^~~~
test.yaml:8:3: job "bar" needs job "unknown" which does not exist in this workflow [job-needs]
  |
8 |   bar:
  |   ^~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNqkjD0OQjEMg/eewttb6AWywRFYEUMLQS0/SdU0YkDcHfUxMTNZsv19KoTmVsJVs1EALqozAGE+G+GQU99gt90f17a7WJyQZ5fh8Z4G21gnG9zsywJxPgl8Koql1CUAOfVfs8tN9Cl/ihv3RzWrKkZ4vT8DADSAPpM=)

<a id="check-parallel-step-refs"></a>

## Parallel steps

Wait and cancel targets are checked against preceding steps declared with `background: true`. A `parallel` group may contain only `run` and `uses` steps.

Example input:

```yaml
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Start server
        id: server
        run: echo 'start'
        background: true
      - run: echo 'tests'
      - cancel: serverr
permissions: {}
```

Output:

```console
test.yaml:11:17: "serverr" is not the ID of a preceding background step. "wait" and "cancel" steps can only refer to an earlier step that has "background: true" [parallel-steps]
   |
11 |       - cancel: serverr
   |                 ^~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNpczksOwjAMBNB9TzG7rnoBX4MTpKlFC60T+cMGcXcUPhFiZcnPo3ERQg1bh0uZjQbA2bxNQENsah5ziMe0p2YvMudq7ytggqSDCSdP6jDWG+uHgG2h/5WGEDivBaO1yNhlTvl61hKyEFyDe8FPpP1gY5ecJPP+7dChsh6b2VbECPfHcwCMcUIJ)

<a id="check-matrix-values"></a>

## Matrix values

Example input:

```yaml
on: push
jobs:
  test:
    strategy:
      matrix:
        node: [10, 12, 14, 14]
        os: [ubuntu-latest, macos-latest]
        exclude:
          - node: 13
            os: ubuntu-latest
          - node: 10
            platform: ubuntu-latest
    runs-on: ${{ matrix.os }}
    steps:
      - run: echo ...
permissions: {}
```

Output:

```console
test.yaml:6:28: duplicate value 14 is found in matrix "node". the same value is at line:6,col:24 [matrix]
  |
6 |         node: [10, 12, 14, 14]
  |                            ^~~
test.yaml:9:19: value 13 in "exclude" does not match in matrix "node" combinations. possible values are 10, 12, 14, 14 [matrix]
  |
9 |           - node: 13
  |                   ^~
test.yaml:12:13: "platform" in "exclude" section does not exist in matrix. available matrix configurations are "node", "os" [matrix]
   |
12 |             platform: ubuntu-latest
   |             ^~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNpskEFrxCAQhe/5Fe/QY5S17cm/svTgZqfdlOiIo7Al5L8XiWkayEHk6Xsf84aDRSzy6L75JrYDMkmuNyA5uUxfP6sCvMtpfG4KCHwni6u59DCvPcx7PR9/3ywW13IrIRc1uYrt4d3A0tTupOcwlTvtZEA1unn797gyD8izyOUQiZPLn5z8WTCVIKpu4GWeWz3NgmVp/SnKNpSqZgsaHgytdRcp+VFk5CAW8/I7ALukWVk=)

[`matrix:`][matrix-doc] defines combinations of multiple values. Nested `include:` and `exclude:` can add/remove specific
combination of matrix values. actionlint checks

- values in `exclude:` appear in the original `matrix:` rows
- duplicate variations of matrix values

GitHub processes `exclude:` before `include:`. Exclusions can only match values in the original rows.
Scalar comparisons use YAML types. `1` and `"1"` are different values, while `1` and `1.0` are the same number.

<a id="check-webhook-events"></a>

## Webhook events validation

Example input:

```yaml
on:
  push:
    # ERROR: Incorrect filter. 'branches' is correct
    branch: foo
    # ERROR: Both 'paths' and 'paths-ignore' filters cannot be used for the same event
    paths: path/to/foo
    paths-ignore: path/to/foo
  issues:
    # ERROR: Incorrect type. 'opened' is correct
    types: created
  release:
    # ERROR: 'tags' filter is not available for 'release' event
    tags: v*.*.*
  # ERROR: Unknown event name
  pullreq:

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ...
permissions: {}
```

Output:

```console
test.yaml:4:5: unexpected key "branch" for "push" section. expected one of "branches", "branches-ignore", "paths", "paths-ignore", "tags", "tags-ignore", "types", "workflows" [syntax-check]
  |
4 |     branch: foo
  |     ^~~~~~~
test.yaml:7:5: both "paths" and "paths-ignore" filters cannot be used for the same event "push". note: use '!' to negate patterns [events]
  |
7 |     paths-ignore: path/to/foo
  |     ^~~~~~~~~~~~~
test.yaml:10:12: invalid activity type "created" for "issues" Webhook event. available types are "assigned", "closed", "deleted", "demilestoned", "edited", "field_added", "field_removed", "labeled", "locked", "milestoned", "opened", "pinned", "reopened", "transferred", "typed", "unassigned", "unlabeled", "unlocked", "unpinned", "untyped" [events]
   |
10 |     types: created
   |            ^~~~~~~
test.yaml:13:5: "tags" filter is not available for release event. it is only for push event [events]
   |
13 |     tags: v*.*.*
   |     ^~~~~
test.yaml:15:3: unknown Webhook event "pullreq". see https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#webhook-events for list of all Webhook event names [events]
   |
15 |   pullreq:
   |   ^~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNpcjkFuxCAMRfec4q8jQfa+DUndQEUxxaZSVc3dRyHZzKy+7Pf1bKnkgDY0nQlsPdY9ET5F5tyiJaUZq8n6svb5qNL5nWbVwXrp7K+xEvbO0fjDAZ0LR+WbxkMJv0tYwjK/KKXzDzn3JdsUGKtd1T6qeqmEsY1qw5d4sonUuN3nAH82CbwnQQjBNe7fWTVLVcL/4zkAzHdIXg==)

Declare events under `on:` to trigger the workflow. [GitHub's event documentation][webhook-doc] lists
the supported webhook events and the activity types allowed in each event's `types:` list.

actionlint validates the Webhook configurations:

- Webhook event name
- types for Webhook event
- filter names
- filter usages
  - `paths` and `paths-ignore`, `branches` and `branches-ignore`, `tags` and `tags-ignore` are exclusive. They can not
    be used for the same event.
  - Some filters are only available for specific events as explained in [the official document][specific-paths-doc]
    (see the following table).

| Filter name       | Events where the filter is available                                         |
| ----------------- | ---------------------------------------------------------------------------- |
| `paths`           | `push`, `pull_request`, `pull_request_target`                                |
| `paths-ignore`    | `push`, `pull_request`, `pull_request_target`                                |
| `branches`        | `merge_group`, `push`, `pull_request`, `pull_request_target`, `workflow_run` |
| `branches-ignore` | `merge_group`, `push`, `pull_request`, `pull_request_target`, `workflow_run` |
| `tags`            | `push`                                                                       |
| `tags-ignore`     | `push`                                                                       |

The available webhooks and their types are listed in [`webhooks.gen.go`](../internal/model/contexts/events/triggers/webhooks.gen.go).
A [script][generate-webhook-events] generates the file, and a weekly CI workflow updates it.

<a id="check-workflow-dispatch-events"></a>

## Workflow dispatch event validation

Example input:

```yaml
on:
  workflow_dispatch:
    inputs:
      # Unknown input type
      id:
        type: text
      # ERROR: No options for 'choice' input type
      kind:
        type: choice
      name:
        type: choice
        options:
          - Tama
          - Mike
        # ERROR: Default value is not in options
        default: Chobi
      message:
        type: string
      verbose:
        type: boolean
        # ERROR: Boolean value must be 'true' or 'false'
        default: yes
      age:
        type: number
        # ERROR: Number value must be parsed as a float number
        default: teen

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      # ERROR: Undefined input
      - run: echo "${{ inputs.massage }}"
      # ERROR: Bool value is not available for object key
      - run: echo "${{ env[inputs.verbose] }}"
      # ERROR: Number value is not available for object key
      - run: echo "${{ env[inputs.age] }}"
      # ERROR: `github.event.inputs` is also not defined
      - run: echo "${{ github.event.inputs.massage }}"
permissions: {}
```

Output:

```console
test.yaml:6:15: input type of workflow_dispatch event must be one of "string", "number", "boolean", "choice", "environment" but got "text" [syntax-check]
  |
6 |         type: text
  |               ^~~~
test.yaml:8:7: input type of "kind" is "choice" but "options" is not set [events]
  |
8 |       kind:
  |       ^~~~~
test.yaml:16:18: default value "Chobi" of "name" input is not included in its options "\"Tama\", \"Mike\"" [events]
   |
16 |         default: Chobi
   |                  ^~~~~
test.yaml:22:18: type of "verbose" input is "boolean". its default value "yes" must be "true" or "false" [events]
   |
22 |         default: yes
   |                  ^~~
test.yaml:26:18: type of "age" input is "number" but its default value "teen" cannot be parsed as a float number: strconv.ParseFloat: parsing "teen": invalid syntax [events]
   |
26 |         default: teen
   |                  ^~~~
test.yaml:33:24: unknown-property: property "massage" is not defined in inputs; did you mean "message"? [expression-reference]
   |
33 |       - run: echo "${{ inputs.massage }}"
   |                        ^~~~~~~~~~~~~~
test.yaml:35:28: operand-type: property access of object must be type of string but got "bool" [expression-type]
   |
35 |       - run: echo "${{ env[inputs.verbose] }}"
   |                            ^~~~~~~~~~~~~~
test.yaml:37:28: operand-type: property access of object must be type of string but got "number" [expression-type]
   |
37 |       - run: echo "${{ env[inputs.age] }}"
   |                            ^~~~~~~~~~
test.yaml:39:24: unknown-property: property "massage" is not defined in github.event.inputs; did you mean "message"? [event-payload]
   |
39 |       - run: echo "${{ github.event.inputs.massage }}"
   |                        ^~~~~~~~~~~~~~~~~~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNqMkcFugzAMhu88hVXtCg/AdefddpumKQEXMogdxQ5dhXj3CZqtmuiq3qzPnw1/zFQXACeOw3Hk00frJBht+hUCOApJ5VIDuPanAtBzwBoUvzSjwdGu3fTsGsyQjMe7AgAHdUxytQBKeDXe/AEvbriOtHg0adQannu2LmOPIqbbfU00OuoynDBalp1jmUc0tN9/RsnwxmZK3mLcDykiFcUn2y2TouhlMiaSkqmGZBNpKkez9raWKIbfFyhXswZseobD0zzng1TebAFhWQ7/mUjTW7Zz1PcHddPdVzunfbIVTkha3fihgNE7ke2SMC/fAwAu9qwi)

Use [`workflow_dispatch`][workflow-dispatch-event] to trigger a workflow manually. It can define parameters called inputs,
each with a name, description, default value and [input type][workflow-dispatch-input-type-announce].

actionlint checks `workflow_dispatch` configuration for errors.

- Input type must be one of 'choice', 'string', 'number', 'boolean', 'environment'
- `options:` must be set for 'choice' input type
- The default value of 'choice' input must be included in options
- The default value of 'boolean' input must be `true` or `false`
- The default value of 'number' input must be parsed as a float number

actionlint types `github.event.inputs` and `inputs` from the input definitions. It reports a type error for properties
not defined in `inputs:`.

For example,

```yaml
inputs:
  string_input:
    type: string
  choice_input:
    type: choice
    options: ["hello"]
  bool_input:
    type: boolean
  num_input:
    type: number
  env_input:
    type: environment
  no_type_input:
```

`inputs` is typed as follows from these definitions:

```json
{
  "string_input": string;
  "choice_input": string;
  "bool_input": bool;
  "num_input": number;
  "env_input": string;
  "no_type_input": any;
}
```

`github.event.inputs` is typed as follows since all properties of it are strings unlike `inputs`:

```json
{
  "string_input": string;
  "choice_input": string;
  "bool_input": string;
  "num_input": string;
  "env_input": string;
  "no_type_input": string;
}
```

<a id="check-glob-pattern"></a>

## Glob filter pattern syntax validation

Example input:

```yaml
on:
  push:
    branches:
      # ^ is not available for branch name. This kind of mistake is usually caused by misunderstanding
      # that regular expression is available here
      - "^foo-"
    tags:
      # Invalid syntax. + cannot follow special character *
      - "v*+"
      # Invalid character range 9-1
      - "v[9-1]"
    paths:
      # GitHub Action's path filter doesn't recognize '.'
      - ./foo/bar.txt

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ...
permissions: {}
```

Output:

```console
test.yaml:6:10: character '^' is invalid for branch and tag names. ref name cannot contain spaces, ~, ^, :, [, ?, *. see `man git-check-ref-format` for more details. note that regular expression is unavailable. note: filter pattern syntax is explained at https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#filter-pattern-cheat-sheet [glob]
  |
6 |       - "^foo-"
  |          ^~~~~~
test.yaml:9:12: invalid glob pattern. unexpected character '+' while checking special character + (one or more). the preceding character must not be special character. note: filter pattern syntax is explained at https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#filter-pattern-cheat-sheet [glob]
  |
9 |       - "v*+"
  |            ^~
test.yaml:11:14: invalid glob pattern. unexpected character '1' while checking character range in []. start of range '9' (57) is larger than end of range '1' (49). note: filter pattern syntax is explained at https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#filter-pattern-cheat-sheet [glob]
   |
11 |       - "v[9-1]"
   |              ^~~
test.yaml:14:9: '.' and '..' are not allowed in glob path. note: filter pattern syntax is explained at https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#filter-pattern-cheat-sheet [glob]
   |
14 |       - ./foo/bar.txt
   |         ^~~~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNpMjEEKAjEMRfc9RZil0g4u7VVEoR06dkST0iQiiHeXaTddhZf3+ITeABTlvF+AWAMuOXEnAAvTbSWyU2MJ99G8D8dpoMvZnq79UYLkoXTzSjTHUJ18xJgHxSYlsfSoKrIl9KBRUdQ+w+6aYkllmKqKHtKSCZxzpqT62pg3Qvbw/f0HAFv0NkQ=)

For filtering branches, tags and paths in Webhook events, [glob syntax][filter-pattern-doc] is available.
actionlint validates glob patterns `branches:`, `branches-ignore:`, `tags:`, `tags-ignore:`, `paths:`, `paths-ignore:` in a
workflow. It checks:

- syntax errors like missing closing brackets for character range `[..]`
- invalid usage like `?` following `*`, invalid character range `[9-1]`, ...
- invalid character usage for Git ref names (branch name, tag name)
  - ref name cannot start/end with `/`
  - ref name cannot contain `[`, `:`, `\`, ...

A common mistake is to use a regular expression in a filter. This check can report that mistake.

<a id="check-cron-syntax-and-timezone"></a>

## CRON syntax and IANA timezone string at `on.schedule`

Example input:

```yaml
on:
  schedule:
    # ERROR: Cron syntax is not correct
    - cron: "0 */3 * *"
    # ERROR: Interval of scheduled job is too small (job runs too frequently)
    - cron: "* */3 * * *"
    # ERROR: Timezone is not a valid IANA timezone string
    - cron: "*/5 * * * *"
      timezone: "Asia/Somewhere"

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ...
permissions: {}
```

Output:

```console
test.yaml:4:13: invalid CRON format "0 */3 * *" in schedule event: expected exactly 5 fields, found 4: [0 */3 * *] [events]
  |
4 |     - cron: "0 */3 * *"
  |             ^~
test.yaml:6:13: scheduled job runs too frequently. it runs once per 60 seconds. the shortest interval is once every 5 minutes [events]
  |
6 |     - cron: "* */3 * * *"
  |             ^~
test.yaml:9:17: invalid timezone "Asia/Somewhere" in schedule event. it must be a valid IANA timezone name [events]
  |
9 |       timezone: "Asia/Somewhere"
  |                 ^~~~~~~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNpkzU2qwzAMBOC9TzF4aXDy4NGNdj1DT5CkgrgkUrBsCi29e3F/oKU7MZ+YUSEH2DTzsS7cbiBiyioE/4fQ/yMg+O88vPNf6XcInwKUtPJFhQl+b2noD7ryeebM3rmTjkYOKGzluZ2rWGxVdaxSalyGZg+ywpvRqzW2TwJPs6LrOrdxXpNZUjHC9XYfABDrONQ=)

To trigger a workflow in specific interval, [scheduled event][schedule-event-doc] can be defined in [POSIX CRON syntax][cron-syntax].

actionlint checks the CRON syntax and frequency of running a job. [The official document][schedule-event-doc] says:

> The shortest interval you can run scheduled workflows is once every 5 minutes.

When the job is run more frequently than once every 5 minutes, actionlint reports it as an error.

actionlint also checks the `timezone` configuration [is a valid IANA timezone string][schedule-item-doc].

<a id="check-runner-labels"></a>

## Runner labels

Unknown-label findings require a complete additional-label set from the host or
`repository.runners`. With unavailable facts, custom labels remain unknown;
known built-in compatibility checks still run. The unknown-label examples below
assume `repository: {runners: []}`.

`ubuntu-latest` accepts Ubuntu 24.04 and 26.04 compatibility during the
[October 19 to November 19, 2026 migration](https://github.blog/changelog/2026-09-17-ubuntu-26-generally-available-and-latest-migration/).
`macos-latest-xlarge` maps to macOS 26.

GitHub [retired `macos-14`, `macos-14-large`, and `macos-14-xlarge` on November 2, 2026](https://github.blog/changelog/2026-10-01-github-actions-macos-14-runner-image-retirement/).
Move hosted jobs to `macos-15`, `macos-26`, or the corresponding
`-xlarge` label; check architecture requirements when replacing the Intel `macos-14-large` image.
These labels are no longer built in. To use them for your own runners, declare them in `repository.runners`.

Example input:

```yaml
on: push
jobs:
  test:
    strategy:
      matrix:
        runner:
          # OK
          - macos-latest
          # ERROR: Unknown runner
          - linux-latest
          # OK: Preset labels for self-hosted runner
          - [self-hosted, linux, x64]
          # OK: Single preset label for self-hosted runner
          - arm64
          # ERROR: Unknown label "gpu". Custom label must be defined in actionlint.yaml config file
          - gpu
    runs-on: ${{ matrix.runner }}
    steps:
      - run: echo ...

  test2:
    # ERROR: Too old macOS worker
    runs-on: macos-10.13
    steps:
      - run: echo ...
permissions: {}
```

Output:

```console
test.yaml:10:13: label "linux-latest" is unknown. available labels are "windows-latest", "windows-latest-8-cores", "windows-2025", "windows-2025-vs2026", "windows-2022", "windows-11-arm", "windows-11-vs2026-arm", "ubuntu-slim", "ubuntu-latest", "ubuntu-latest-4-cores", "ubuntu-latest-8-cores", "ubuntu-latest-16-cores", "ubuntu-26.04", "ubuntu-26.04-arm", "ubuntu-24.04", "ubuntu-24.04-arm", "ubuntu-22.04", "ubuntu-22.04-arm", "xcode-27", "xcode-27-xlarge", "macos-latest", "macos-latest-xlarge", "macos-latest-large", "macos-26-intel", "macos-26-xlarge", "macos-26-large", "macos-26", "macos-15-intel", "macos-15-xlarge", "macos-15-large", "macos-15", "self-hosted", "x64", "arm", "arm64", "linux", "macos", "windows". if it is a custom label for self-hosted runner, set list of labels in actionlint.yaml config file [runner-label]
   |
10 |           - linux-latest
   |             ^~~~~~~~~~~~
test.yaml:16:13: label "gpu" is unknown. available labels are "windows-latest", "windows-latest-8-cores", "windows-2025", "windows-2025-vs2026", "windows-2022", "windows-11-arm", "windows-11-vs2026-arm", "ubuntu-slim", "ubuntu-latest", "ubuntu-latest-4-cores", "ubuntu-latest-8-cores", "ubuntu-latest-16-cores", "ubuntu-26.04", "ubuntu-26.04-arm", "ubuntu-24.04", "ubuntu-24.04-arm", "ubuntu-22.04", "ubuntu-22.04-arm", "xcode-27", "xcode-27-xlarge", "macos-latest", "macos-latest-xlarge", "macos-latest-large", "macos-26-intel", "macos-26-xlarge", "macos-26-large", "macos-26", "macos-15-intel", "macos-15-xlarge", "macos-15-large", "macos-15", "self-hosted", "x64", "arm", "arm64", "linux", "macos", "windows". if it is a custom label for self-hosted runner, set list of labels in actionlint.yaml config file [runner-label]
   |
16 |           - gpu
   |             ^~~
test.yaml:23:14: label "macos-10.13" is unknown. available labels are "windows-latest", "windows-latest-8-cores", "windows-2025", "windows-2025-vs2026", "windows-2022", "windows-11-arm", "windows-11-vs2026-arm", "ubuntu-slim", "ubuntu-latest", "ubuntu-latest-4-cores", "ubuntu-latest-8-cores", "ubuntu-latest-16-cores", "ubuntu-26.04", "ubuntu-26.04-arm", "ubuntu-24.04", "ubuntu-24.04-arm", "ubuntu-22.04", "ubuntu-22.04-arm", "xcode-27", "xcode-27-xlarge", "macos-latest", "macos-latest-xlarge", "macos-latest-large", "macos-26-intel", "macos-26-xlarge", "macos-26-large", "macos-26", "macos-15-intel", "macos-15-xlarge", "macos-15-large", "macos-15", "self-hosted", "x64", "arm", "arm64", "linux", "macos", "windows". if it is a custom label for self-hosted runner, set list of labels in actionlint.yaml config file [runner-label]
   |
23 |     runs-on: macos-10.13
   |              ^~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNqEj81qxCAUhfc+xVl0OUqnHWbhq5QubHqbWOIPXoWUkHcvoqEIhVnJ8d57+L7gNWLhRXyHD9YCyMS5vgDnZDLNPy0BzuRktzMBqXhP6S8DEs5MgeVqasswWK0v23+DN6b1Sy6BM31e2toF2/32PmyZ5O634WeORXQKltXiad87ompkOI7uQZFPTFkPNGhaApRSohu/6LGseVyf1fX1cUek5CyzDZ419uN3AMzLWyg=)

GitHub Actions jobs run on [GitHub-hosted][gh-hosted-runner] or [self-hosted][self-hosted-runner] runners.
Each runner has one or more labels. GitHub selects a runner with the labels specified in `runs-on:`.

actionlint checks labels in `runs-on:`. For an expression such as
`runs-on: ${{ matrix.foo }}`, it determines the possible values and checks each one.

If your self-hosted runners use custom labels, add the labels to the [`actionlint.yaml` configuration file](config.md)
so actionlint can recognize them.

When `runs-on:` lists several labels, GitHub selects a runner that has all of them.
actionlint reports conflicting combinations.

Example input:

```yaml
on: push
jobs:
  test:
    runs-on: [ubuntu-latest, windows-latest]
    steps:
      - run: echo ...
permissions: {}
```

Output:

```console
test.yaml:4:30: label "windows-latest" conflicts with label "ubuntu-latest" defined at line:4,col:15. note: to run your job on each workers, use matrix [runner-label]
  |
4 |     runs-on: [ubuntu-latest, windows-latest]
  |                              ^~~~~~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNosi8ENAjEMBP+pYgsgKSCtIB4cWLogsKNbW/dA9I4M91qtZsa0YwbX8rCFvQAu9FxgC2VNfo4l1KM+rwlP2IfebefxLz+ZLpP/DqjZdshtNbTWypTtNchhyo735zsAgkclJA==)

To run a job on multiple runners, define an axis in `matrix:` and reference it with `${{ }}` in `runs-on:`.
Listing several labels directly in `runs-on:` selects one runner that has all those labels.

<a id="check-action-format"></a>

## Action format in `uses:`

Example input:

```yaml
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      # ERROR: ref is missing
      - uses: actions/checkout
      # ERROR: owner name is missing
      - uses: checkout@v2
      # ERROR: tag is empty
      - uses: "docker://image:"
      # ERROR: local action must start with './'
      - uses: .github/my-actions/do-something
permissions: { contents: read }
```

Output:

```console
test.yaml:7:15: invalid-reference: specifying action "actions/checkout" in invalid format because ref is missing. available formats are "{owner}/{repo}@{ref}", "{owner}/{repo}/{path}@{ref}", "./{path}", or "$/{path}" [action]
  |
7 |       - uses: actions/checkout
  |               ^~~~~~~~~~~~~~~~
test.yaml:9:15: invalid-reference: specifying action "checkout@v2" in invalid format because owner is missing. available formats are "{owner}/{repo}@{ref}", "{owner}/{repo}/{path}@{ref}", "./{path}", or "$/{path}" [action]
  |
9 |       - uses: checkout@v2
  |               ^~~~~~~~~~~
test.yaml:11:15: invalid-reference: tag of Docker action should not be empty: "docker://image" [action]
   |
11 |       - uses: "docker://image:"
   |               ^~~~~~~~~~~~~~~~~
test.yaml:13:15: invalid-reference: specifying action ".github/my-actions/do-something" in invalid format because ref is missing. available formats are "{owner}/{repo}@{ref}", "{owner}/{repo}/{path}@{ref}", "./{path}", or "$/{path}" [action]
   |
13 |       - uses: .github/my-actions/do-something
   |               ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNpczcGKwzAMBNB7vkLk7jXsUaf9FccRsTe1FCypUEr/vbgll5wG5g2MMMLhWqZ/WRQnACO1kQDdWcNwX5zNwy0N+5AaHfpdAQRwJUVI2aqwxlwo7+J24bP+u/9eZF4l79QxxtrSRjhf/GerVnyJ7RHOj1WCSiMrlbfpoN6q6gCEJ2RhIzZF6JRWeL0HAO2eR9o=)

The `uses:` value must follow one of [GitHub's action reference formats][action-uses-doc]:

- action hosted on GitHub: `owner/repo/path@ref`
- local action: `./path/to/my-action`
- action in the workflow's repository at the running commit: `$/path/to/my-action`
- Docker action: `docker://image:tag`

actionlint checks that each `uses:` value follows one of these formats.

Leading slashes after the `$/` prefix are stripped, so `$//path/to/my-action` and `$/path/to/my-action` name the same action.
For actions, `$/`, `$//`, and `$///` refer to the repository root. Reusable workflow calls still require a workflow file path.
What the path is relative to depends on where the `uses:` is written: a step in a workflow resolves against the workflow's
repository at the running commit, and a step in a composite action resolves against the repository and ref that action was
loaded from. The runner also needs its `actions_self_repository` feature flag enabled; when it is off the job fails during
Setup Job with an unhandled exception instead of a workflow annotation.

If actionlint cannot find a referenced local action, it reports `missing-local-action`. An action cloned during the workflow may
therefore need a local source mapping or a documented exception. See rhysd/actionlint#25 and rhysd/actionlint#40 for context.

<a id="check-local-action-inputs"></a>

## Local action inputs validation at `with:`

My action definition at `.github/actions/my-action/action.yaml`:

```yaml
name: "My action"
author: "rhysd <https://rhysd.github.io>"
description: "my action"

inputs:
  name:
    description: your name
    default: anonymous
  message:
    description: message to this action
    required: true
  addition:
    description: additional information
    required: false

runs:
  using: "node24"
  main: "index.js"
```

Example input:

```yaml
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      # missing required input "message"
      - uses: ./.github/actions/my-action
      # unexpected input "additions"
      - uses: ./.github/actions/my-action
        with:
          name: rhysd
          message: hello
          additions: foo, bar
permissions: {}
```

Output:

<!-- Skip update output -->

```console
test.yaml:7:15: missing input "message" which is required by action "My action" defined at "./.github/actions/my-action". all required inputs are "message" [action]
  |
7 |       - uses: ./.github/actions/my-action
  |               ^~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:13:11: input "additions" is not defined in action "My action" defined at "./.github/actions/my-action". available inputs are "addition", "message", "name" [action]
   |
13 |           additions: foo, bar
   |           ^~~~~~~~~~
```

<!-- Skip playground link -->

For a step that uses a local or self-repository action, actionlint reads its repository metadata file and
checks `with:` for missing required inputs and unexpected input names.

<a id="check-popular-action-inputs"></a>

## Remote action inputs and outputs

Remote reference syntax is checked without metadata. Input names, required inputs
and declared outputs are checked when actionlint acquires the action manifest.
Metadata-dependent checks use the same request-scoped service and selected content.

Online lookups use the bundled Commitish WebAssembly plugin to resolve branches,
tags and commit IDs. When metadata is unavailable, actionlint skips checks that
need the manifest. Local action validation uses the local manifest. See
[remote action metadata](remote-actions.md).

<a id="detect-outdated-popular-actions"></a>

## Action runtime lifecycle

Available manifests are checked against runtime tables generated from GitHub's
runner source. Deprecated runtimes do not disable input/output validation.
The declared runtime determines the finding; an action's tag or release age does
not. Unfetched metadata does not establish that a runtime or service is retired.

The provider records the requested ref, the Git object it selected, the final commit,
the repository identity and how it looked up the ref. A tag object can point to a different commit object.
These records describe what actionlint fetched. They do not record what a runner executed.

<a id="check-shell-names"></a>

## Shell name validation at `shell:`

Example input:

```yaml
on: push
jobs:
  linux:
    runs-on: ubuntu-latest
    steps:
      - run: echo 'hello'
        # ERROR: Unavailable shell
        shell: dash
      - run: echo 'hello'
        # ERROR: 'powershell' is only available on Windows
        shell: powershell
  mac:
    runs-on: macos-latest
    defaults:
      run:
        # ERROR: default config is also checked. fish is not supported
        shell: fish
    steps:
      - run: echo 'hello'
        # OK: Custom shell
        shell: "perl {0}"
  windows:
    runs-on: windows-latest
    steps:
      - run: echo 'hello'
        # ERROR: 'sh' is only available on Windows
        shell: sh
      - run: echo 'hello'
        # OK: 'powershell' is only available on Windows
        shell: powershell
permissions: {}
```

Output:

```console
test.yaml:8:16: shell name "dash" is invalid. available names are "bash", "pwsh", "python", "sh" [shell-name]
  |
8 |         shell: dash
  |                ^~~~
test.yaml:11:16: shell name "powershell" is invalid on macOS or Linux. available names are "bash", "pwsh", "python", "sh" [shell-name]
   |
11 |         shell: powershell
   |                ^~~~~~~~~~
test.yaml:17:16: shell name "fish" is invalid. available names are "bash", "pwsh", "python", "sh" [shell-name]
   |
17 |         shell: fish
   |                ^~~~
test.yaml:27:16: shell name "sh" is invalid on Windows. available names are "bash", "cmd", "powershell", "pwsh", "python" [shell-name]
   |
27 |         shell: sh
   |                ^~
```

[Playground](https://kjanat.github.io/actionlint/#eNqkkEGKhTAQRPc5ReHGlTDr3CZqSzLEdEincUC8+xBHho+rj39XST2SR3GyyCrefPMo1gAxJP1pASiaZGiAjpqqDtFVknpWUinLHwUMjbSgyTN6TzFyfzWAtLPF7MS/T2feqJzZAKubbjqrm1hebWZanMb6L9Q+uL+5BPGP1LtMJWL/OjoDbCHNvMlN6Lr9ZKCH82QqaxAJnMRiP34HAFUzhLI=)

[GitHub documents the available runner shells][shell-doc]. actionlint checks the shell names in `shell:` settings.

<a id="check-job-step-ids"></a>

## Job ID and step ID uniqueness

Example input:

```yaml
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo 'hello'
        id: step_id
      - run: echo 'bye'
        # ERROR: Duplicate step ID
        id: STEP_ID
  # ERROR: Duplicate job ID
  TEST:
    runs-on: ubuntu-latest
    steps:
      - run: echo 'hello'
        # OK. Step ID uniqueness is job-local
        id: step_id
permissions: {}
```

Output:

```console
test.yaml:10:13: step ID "STEP_ID" duplicates. previously defined at line:7,col:13. step ID must be unique within a job. note that step ID is case insensitive [id]
   |
10 |         id: STEP_ID
   |             ^~~~~~~
test.yaml:12:3: key "TEST" is duplicated in "jobs" section. previously defined at line:3,col:3. note that this key is case insensitive [syntax-check]
   |
12 |   TEST:
   |   ^~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNq0zqEOwjAQxnG/p/jcVF/gNBM4ktUvlF3SktJrdj1BCO9OOjAkWNQn/j/xSSFU0zhcJSgNQGNtfYHNirreLVhp5vK5tz1p46pvBbguCXyJgjFyzjJ+CpBW2vGS1l863Pnbzn46LcfDAPhp9v/5UXm7JdUkRQmP52sA8+RJgg==)

Job IDs must be unique in a workflow, and step IDs must be unique within each job. actionlint compares IDs without regard to case and reports duplicates.

<a id="check-hardcoded-credentials"></a>

## Hardcoded credentials

Example input:

```yaml
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    container:
      image: "example.com/owner/image"
      credentials:
        username: user
        # ERROR: Hardcoded password
        password: pass
    services:
      redis:
        image: redis
        credentials:
          username: user
          # ERROR: Hardcoded password
          password: pass
    steps:
      - run: echo 'hello'
permissions: {}
```

Output:

```console
test.yaml:10:19: "password" section in "container" section should be specified via secrets. do not put password value directly [credentials]
   |
10 |         password: pass
   |                   ^~~~
test.yaml:17:21: "password" section in "redis" service should be specified via secrets. do not put password value directly [credentials]
   |
17 |           password: pass
   |                     ^~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNp0kEFuxCAMRfc5hTWbWaWz5zYM+WqowEY2NJWq3r2CpnSTrrDe8+cjhB2VZvvyJk9zC1GF1X4SaWNbu2/PxrWtyXc3VBCuPjL0Z5MoZv8KRzd8+FwSXoLkhxwMfQxzO9eCYgPX6JP9JomaQdlnuDFNXLzZIbq5MQ1s0PcYMLOKLdrfRecjBp3wsvLf0uvaijLDa/8XRwi70H1HSnJfCjRHsyhsjj6/vgcAV5hj0Q==)

[Credentials for container][credentials-doc] can be put in `container:` configuration. Password should be put in secrets
and the value should be expanded with `${{ }}` syntax at `password:`. actionlint checks hardcoded credentials, and reports
them as an error.

<a id="check-env-var-names"></a>

## Environment variable names

Example input:

```yaml
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    env:
      FOO=BAR: foo
      FOO BAR: foo
    steps:
      - run: echo 'hello'
permissions: {}
```

Output:

```console
test.yaml:6:7: environment variable name "FOO=BAR" is invalid. '&', '=' and spaces should not be contained [env-var]
  |
6 |       FOO=BAR: foo
  |       ^~~~~~~~
test.yaml:7:7: environment variable name "FOO BAR" is invalid. '&', '=' and spaces should not be contained [env-var]
  |
7 |       FOO BAR: foo
  |       ^~~
```

[Playground](https://kjanat.github.io/actionlint/#eNpUjDGqAkEQRPM5RWUbzQUafvANTBe8gSstszJ2D1vTJuLdZRQRo4L3qspN0IIlXXyhJKAr+0hgC2MePpawHrkeh3sptdu7A+zn+W/3fxCc3b8IP4hdGz+DPI4FeiqOqWitPqWm23UlVzcK7o/nAHd8Kt8=)

Environment variable names must not contain `=`. Avoid `&` and spaces too, because
they have special meanings in shell syntax and usually indicate a mistake in a variable name.

actionlint checks environment variable names under `env:`.

<a id="cache-mode"></a>

## Cache access

Example input:

```yaml
on: push
cache-mode: restore
jobs:
  build:
    runs-on: ubuntu-latest
    cache-mode: read-write
    steps:
      - run: echo ok
permissions: {}
```

Output:

```console
test.yaml:2:13: "restore" is invalid for "cache-mode". expected one of "read", "write", "write-only", "none" [syntax-check]
  |
2 | cache-mode: restore
  |             ^~~~~~~
test.yaml:6:17: "read-write" is invalid for "cache-mode". expected one of "read", "write", "write-only", "none" [syntax-check]
  |
6 |     cache-mode: read-write
  |                 ^~~~~~~~~~
```

<!-- Skip playground link -->

The [`cache-mode` key](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#cache-mode)
controls cache access independently of `permissions`. actionlint accepts it at workflow level and on ordinary jobs
or jobs that call reusable workflows. It checks that the value is one of these strings:

| Value        | Restore caches | Save caches |
| ------------ | -------------- | ----------- |
| `read`       | Yes            | No          |
| `write`      | Yes            | Yes         |
| `write-only` | No             | Yes         |
| `none`       | No             | No          |

A job's declaration overrides its workflow's declaration. Omitted settings use GitHub's trigger-dependent
defaults. Explicit `write` and `write-only` declarations are valid even on low-trust events; GitHub warns about
their security implications. Cache operations skipped by the selected mode do not fail the workflow.

For local reusable workflows referenced through `./` or `$/`, actionlint checks that the called jobs' declarations
fit within the caller's explicit limit. The check follows nested local calls, including intermediate workflows
that omit `cache-mode`. It compares restore and save access separately: a `read` caller cannot grant `write-only`,
and a `write-only` caller cannot grant `read`. Diagnostics identify the called job and point to the caller's `uses`.

An unspecified caller mode leaves the callee free to declare its own mode, including write access on a low-trust
trigger. Invalid declarations produce syntax diagnostics and are excluded from access comparisons. Cache access
validation covers workflows available in the local repository.

<a id="cache-policies"></a>

## Cache safety policies

Example input:

```yaml
on: pull_request_target
jobs:
  call:
    uses: example/repository/.github/workflows/build.yml@main
  build:
    runs-on: ubuntu-latest
    cache-mode: write
    steps:
      - run: echo hello
  restore:
    runs-on: ubuntu-latest
    cache-mode: none
    steps:
      - uses: actions/cache/restore@v5
        with: { path: .cache, key: build }
permissions: {}
```

Output:

```console
test.yaml:4:11: reusable workflow call "example/repository/.github/workflows/build.yml@main" has no explicit cache access limit for low-trust trigger(s) "pull_request_target". the callee can request writes despite the trigger's read-only default. set "cache-mode: read" or "cache-mode: none" on this job or workflow [cache-call-unrestricted]
  |
4 |     uses: example/repository/.github/workflows/build.yml@main
  |           ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:7:17: cache-mode "write" grants cache writes for low-trust trigger(s) "pull_request_target". untrusted code or input can poison caches consumed by privileged workflows. use "read" or "none", or document a reviewed exception with an inline suppression [cache-write-untrusted]
  |
7 |     cache-mode: write
  |                 ^~~~~
test.yaml:14:15: "actions/cache/restore" cannot restore caches with effective cache-mode "none". GitHub skips the operation; remove this cache step or select an action and mode that match the job's intended cache access [cache-operation]
   |
14 |       - uses: actions/cache/restore@v5
   |               ^~~~~~~~~~~~~~~~~~~~~~~~
```

<!-- Skip playground link -->

These three policies are enabled by default. `cache-write-untrusted` reports write grants on low-trust events that
can use default-branch caches. `cache-call-unrestricted` requires an explicit cap on reusable calls from those events,
because the callee can otherwise request write access. `cache-operation` reports official cache steps that an
explicit mode makes ineffective. Omitting `cache-mode` on an ordinary job uses GitHub's trigger default and
produces no finding. Ordinary pull request caching is not treated as the default-branch cache risk.

See [policy configuration and inline exceptions](config.md#policy-checks)
for the event list, exact action/mode combinations, limitations, and ways to document reviewed exceptions.
Use [`forbidden-suppression`](config.md#forbidden-suppression) to prohibit those inline exceptions for all
or selected cache rules, reporting the prohibited comment, the original violation, or both.

<a id="permissions"></a>

## Permissions

Example input:

```yaml
on: push

# ERROR: Available values for whole permissions are "write-all", "read-all" or "none"
permissions: write

jobs:
  test:
    runs-on: ubuntu-latest
    permissions:
      # ERROR: "checks" is correct scope name
      check: write
      # ERROR: Available values are "read", "write" or "none"
      issues: readable
      # ERROR: "models" doesn't have "write" scope
      models: write
      # ERROR: "vulnerability-alerts" only supports "read" or "none"
      vulnerability-alerts: write
    steps:
      - run: echo hello
```

Output:

```console
test.yaml:4:14: "write" is invalid for permission for all the scopes. available values are "read-all", "write-all" or {} [permissions]
  |
4 | permissions: write
  |              ^~~~~
test.yaml:11:7: unknown permission scope "check". all available permission scopes are "actions", "artifact-metadata", "attestations", "checks", "code-quality", "contents", "copilot-requests", "deployments", "discussions", "drives", "id-token", "issues", "models", "packages", "pages", "pull-requests", "repository-projects", "security-events", "statuses", "vulnerability-alerts" [permissions]
   |
11 |       check: write
   |       ^~~~~~
test.yaml:13:15: "readable" is invalid as permission of scope "issues". available values are "read", "write", "none" [permissions]
   |
13 |       issues: readable
   |               ^~~~~~~~
test.yaml:15:15: "write" is invalid as permission of scope "models". available values are "read", "none" [permissions]
   |
15 |       models: write
   |               ^~~~~
test.yaml:17:29: "write" is invalid as permission of scope "vulnerability-alerts". available values are "read", "none" [permissions]
   |
17 |       vulnerability-alerts: write
   |                             ^~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNpUjtGtwyAMRf+ZwguwANtAYgnecwD52q26fUWqVM2XpXOvjz16oumoIUzWowFtdCR6ajMO4W8UpEBkDFuTSL0jriUv3s2j5JWd0a/gBERb5e3/sn1QA5yRSDnvuciFj7Gz4F59uHTWXJo0e8UsrHZrwHh+T8X1WiLe6qDKIuMdAAD//6u/RE0=)

Set [`permissions:`][perm-config-doc] on a workflow or job to control access through `GITHUB_TOKEN`.
Each permission scope has its own access levels. The default and available levels are described in
[the document][permissions-doc].

actionlint checks permission scopes and access levels in the workflow.

<a id="check-reusable-workflows"></a>

## Reusable workflows

[Reusable workflows][reusable-workflow-doc] is a feature to call a workflow from another workflow.

actionlint does several checks for both workflow calls (caller) and reusable workflows (callee):

- syntax of workflow calls and reusable workflows
- type checks for inputs (respecting `type:` field of each input) in both workflow calls and reusable workflows
- type checks for `inputs`, `outputs` and `secrets` context objects in reusable workflows
- optional/required/undefined inputs and secrets at `uses:` in workflow calls
- type checks for `outputs` objects used by downstream jobs of workflow calls
- permissions required by the jobs of a reusable workflow against the permissions the calling job grants

These checks are described in this section.

### Check input definitions of `workflow_call` event in reusable workflow

Example input:

```yaml
on:
  workflow_call:
    inputs:
      scheme:
        description: Scheme of URL
        # OK: Type is string
        default: https
        type: string
      host:
        default: example.com
        type: string
      port:
        description: Port of URL
        # ERROR: Type is number but default value is string
        default: ":1234"
        type: number
      query:
        description: Query of URL
        # ERROR: Type must be one of number, string, boolean
        type: object
      path:
        description: Path of URL
        required: true
        # ERROR: Default value is never used since this input is required
        default: ""
        type: string
jobs:
  do:
    runs-on: ubuntu-latest
    steps:
      - run: echo "${{ inputs.scheme }}://${{ inputs.host }}:${{ inputs.port }}${{ inputs.path }}"
permissions: {}
```

Output:

```console
test.yaml:15:18: input of workflow_call event "port" is typed as number but its default value ":1234" cannot be parsed as a float number: strconv.ParseFloat: parsing ":1234": invalid syntax [events]
   |
15 |         default: ":1234"
   |                  ^~~~~~~
test.yaml:20:15: invalid value "object" for input type of workflow_call event. it must be one of "boolean", "number", or "string" [syntax-check]
   |
20 |         type: object
   |               ^~~~~~
test.yaml:25:18: input "path" of workflow_call event has the default value "", but it is also required. if an input is marked as required, its default value will never be used [events]
   |
25 |         default: ""
   |                  ^~
test.yaml:31:24: unknown-script-value: interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution [script-interpolation]
   |
31 |       - run: echo "${{ inputs.scheme }}://${{ inputs.host }}:${{ inputs.port }}${{ inputs.path }}"
   |                        ^~~~~~~~~~~~~
test.yaml:31:47: unknown-script-value: interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution [script-interpolation]
   |
31 |       - run: echo "${{ inputs.scheme }}://${{ inputs.host }}:${{ inputs.port }}${{ inputs.path }}"
   |                                               ^~~~~~~~~~~
test.yaml:31:84: unknown-script-value: interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution [script-interpolation]
   |
31 |       - run: echo "${{ inputs.scheme }}://${{ inputs.host }}:${{ inputs.port }}${{ inputs.path }}"
   |                                                                                    ^~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNp8kU1uAjEMhfecwhp1C6g/q5yhi/6o62ommCY0EwfbEUUod6/CABpRprvks/3y4kfRzAB2xN/rQLtP24ZQAYCPKasMZwCxDns83wBWKJZ9Uk/RwPuxCLSGj7fnUcu6zUENONUkF6z7hAZE2cevE3Qkav7O4U/bp4ALS/1/04lYJ4y9EOukrcbcPzw+NVfSMfcd8gluM/J+Qvu11q7FBw3qNmj1bK9VN2WvVXetwLjNnnFlQDnjDdfN7V1sqDuGtSJzbOAcZV4fyV2OmuehVZTBkyimS7Dz2mkArSNo7g6HU+6LIXAoxSyXI1yjqnCE6v6hlDGp/yqlmSXk3ot4imLgUH4HAL68tMQ=)

Unlike action inputs, workflow inputs must specify their types. actionlint validates the types and default values.
See [GitHub's documentation][create-reusable-workflow-doc] for details.

### Check workflow call syntax

Example input:

```yaml
on: push
jobs:
  job1:
    uses: owner/repo/path/to/workflow.yml@v1
    # ERROR: 'runs-on' is not available on calling reusable workflow
    runs-on: ubuntu-latest
  job2:
    # ERROR: Local file path with ref is not available
    uses: ./.github/workflows/ci.yml@main
  job3:
    # ERROR: 'with' is only available on calling reusable workflow
    with:
      foo: bar
    runs-on: ubuntu-latest
    steps:
      - run: echo hello
  job4:
    # ERROR: This workflow does not exist
    uses: ./.github/workflows/not-existing.yml
permissions: {}
```

Output:

```console
test.yaml:6:5: when a reusable workflow is called with "uses", "runs-on" is not available. only following keys are allowed: "name", "uses", "with", "secrets", "needs", "if", "permissions", "cache-mode", "strategy", and "concurrency" in job "job1" [syntax-check]
  |
6 |     runs-on: ubuntu-latest
  |     ^~~~~~~~
test.yaml:9:11: reusable workflow call "./.github/workflows/ci.yml@main" at "uses" is not following the format "owner/repo/path/to/workflow.yml@ref", "./path/to/workflow.yml", nor "$/path/to/workflow.yml". see https://docs.github.com/en/actions/learn-github-actions/reusing-workflows for more details [workflow-call]
  |
9 |     uses: ./.github/workflows/ci.yml@main
  |           ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:12:5: "with" is only available for a reusable workflow call with "uses" but "uses" is not found in job "job3" [syntax-check]
   |
12 |     with:
   |     ^~~~~
test.yaml:19:11: could not read reusable workflow file for "./.github/workflows/not-existing.yml": open /path/to/repo/.github/workflows/not-existing.yml: no such file or directory [workflow-call]
   |
19 |     uses: ./.github/workflows/not-existing.yml
   |           ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNqEzkFORCEMBuD9O0UvwCOjrlh5FZh0ho68ltAiGuPdDUM0rnQFi///+gsHqF3zdpOkYQO4STrNF6AragAZjM03rOJrtOxN/JD2ciky9vejPL+e7uHWWd3Eeups3ZVoqLa8h9/e7vcrWe7ph1F/pjt1ROLVeFyNQZbXD+AiEiDF9vc1ADWs+l1yMxkAz1kgYymy+Kf/BrGYwzdSI77OaVvFdpAqCWuAj8+vAQBshmLg)

Jobs that call reusable workflows accept [a specific set of keys][reusable-workflow-call-keys].
For example, a caller can pass `secrets:`, but a job that runs steps cannot.
A caller cannot set `runs-on:` because the called workflow selects its runners.
actionlint checks each job's keys according to whether it calls a workflow or runs steps.

The workflow reference in `uses:` must follow the format `owner/repo/path/to/workflow.yml@ref`,
`./path/to/workflow.yml`, or `$/path/to/workflow.yml` as described in [the official document][create-reusable-workflow-doc].
actionlint checks if the value follows the format.

For a local or self-repository reference starting with `./` or `$/`, actionlint also checks
that the called workflow file exists and reports an error if it is missing.

### Check types of `inputs.*` and `secrets.*` in reusable workflow

Example input:

```yaml
on:
  workflow_call:
    inputs:
      url:
        description: "your URL"
        type: string
      lucky_number:
        description: "your lucky number"
        type: number
    secrets:
      credential:
        description: "your credential"

jobs:
  test:
    runs-on: ubuntu-24.04
    steps:
      - name: Send data
        # ERROR: uri is typo of url
        run: curl ${{ inputs.uri }} -d ${{ inputs.lucky_number }}
        env:
          # ERROR: credentials is typo of credential
          TOKEN: ${{ secrets.credentials }}
permissions: {}
```

Output:

```console
test.yaml:20:23: unknown-property: property "uri" is not defined in inputs; did you mean "url"? [expression-reference]
   |
20 |         run: curl ${{ inputs.uri }} -d ${{ inputs.lucky_number }}
   |                       ^~~~~~~~~~
test.yaml:20:44: unknown-script-value: interpolated scalar is not proved to occupy a script data argument; pass it through an environment variable and read that variable as data instead [script-interpolation]
   |
20 |         run: curl ${{ inputs.uri }} -d ${{ inputs.lucky_number }}
   |                                            ^~~~~~~~~~~~~~~~~~~
test.yaml:23:22: unknown-property: property "credentials" is not defined in secrets; did you mean "credential"? [expression-reference]
   |
23 |           TOKEN: ${{ secrets.credentials }}
   |                      ^~~~~~~~~~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNp8ULtqxEAM7P0Vg0lrE8JV26dKSCCP+rDXStjcWmu0qxzG+N+DH7GPK64To3loFNhkwDnI6cuH89FW3k8A4LjTFJcZUPH/I9BQtOK65AIb5H1Qwefbc77tU9+RQUzi+HsFvdpTf2Rta5KbRjMRC/HacUFnMJIV2s+zQg1xctXtK3danmU/oZ71iWJaVKIci4mttXLS4uFQ3h+WuETdFlaAq5YM3okbNFWqtkhRNrAqHnfDsH6wVHEYRxTNJXj5D4zj5kD8uzcAPl6fHl/MLFwbl3uHOAk7ktbF6AJHg2H8GwAE1Yqg)

Inputs of reusable workflow calls are set to `inputs.*` properties following the definitions at `on.workflow_call.inputs`.
And in a job of a reusable workflow, `secrets.*` are passed from caller of the workflow so it is set following the definitions at
`on.workflow_call.secrets`. See [the official document][create-reusable-workflow-doc] for more details.

actionlint contextually defines types of `inputs` and `secrets` contexts looking at `workflow_call` event. Keys of `inputs` only
allow keys at `on.workflow_call.inputs` and their values are typed based on `on.workflow_call.inputs.<input_name>.type`. Type of
`secrets` is also strictly typed following `on.workflow_call.secrets`.

[From May 3, 2022][inherit-secrets-announce], GitHub Actions allows inheriting secrets by calling reusable workflows. The caller
declares to inherit all secrets.

```yaml
jobs:
  pass-secrets-to-workflow:
    uses: ./.github/workflows/called-workflow.yml
    secrets: inherit
```

This means that actionlint cannot know whether the workflow inherits secrets or not when checking a reusable workflow.
To solve this issue, actionlint assumes that

- when `secrets:` is omitted in a reusable workflow, the workflow inherits secrets from a caller
- when `secrets:` exists in a reusable workflow, the workflow inherits no other secret

Following the assumptions,

```yaml
on:
  workflow_call:

jobs:
  pass-secret-to-action:
    runs-on: ubuntu-latest
    steps:
      # OK: This reports no error. FOO is assumed to be inherited from caller
      - run: echo ${{ secrets.FOO }}
permissions: {}
```

this workflow causes no error. And

```yaml
on:
  workflow_call:
    secrets:

jobs:
  pass-secret-to-action:
    runs-on: ubuntu-latest
    steps:
      # ERROR: Secret FOO is not defined
      - run: echo ${{ secrets.FOO }}
permissions: {}
```

this workflow causes 'no such secret' error at `secrets.FOO`.

### Check outputs in reusable workflow

Example input:

```yaml
on:
  workflow_call:
    outputs:
      image-version:
        description: "Docker image version"
        # ERROR: 'imagetag' does not exist (typo of 'image_tag')
        value: ${{ jobs.gen-image-version.outputs.imagetag }}
jobs:
  gen-image-version:
    runs-on: ubuntu-latest
    outputs:
      image_tag: "${{ steps.get_tag.outputs.tag }}"
    steps:
      - run: ./output_image_tag.sh
        id: get_tag
permissions: {}
```

Output:

```console
test.yaml:7:20: unknown-property: property "imagetag" is not defined in jobs.gen-image-version.outputs; did you mean "image_tag"? [expression-reference]
  |
7 |         value: ${{ jobs.gen-image-version.outputs.imagetag }}
  |                    ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNp0j8FuwyAQRO/+ipHVK/TOuf9hEWfr0hCw2CU5WP73aoONVFU9MnrMm83JDcAzl9tnzM9p9jFqAOQqaxVuDyDc/ULmQYVDTmcIXInnElbREONHnm9UGouDHTv78LGSw9u24Ttf2C6UzK9aezjtKxW/YN8HRdX3h24jSk1sVF4vNUk10Qux/HvBJH5xGHUDC606QjTr6mZto1/E+d2oysG+N3LqdZa/+onh6nA0DiuVe2Bdyg7b/jMA8gR0ew==)

Define outputs of a reusable workflow at `on.workflow_call.outputs`, as described in [the GitHub documentation][reusable-workflow-outputs].
Use the `jobs` context to refer to job outputs when defining a workflow output. actionlint checks that the context is used correctly.

### Check inputs and secrets in workflow call

Example reusable workflow:

```yaml
# .github/workflows/reusable.yaml
on:
  workflow_call:
    inputs:
      name:
        type: string
        required: true
      id:
        type: number
      message:
        type: string
    secrets:
      password:
        required: true

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo '${{ outputs.required_input }}'
permissions: {}
```

Example input:

```yaml
on: push

jobs:
  # Check required/undefined inputs and secrets
  missing-required:
    uses: ./.github/workflows/reusable.yaml
    with:
      # ERROR: Undefined input
      user: rhysd
      # ERROR: Required input "name" is missing
    secrets:
      # ERROR: Undefined secret
      credentials: my-token
      # ERROR: Required secret "password" is missing

  # Check types of inputs defined in reusable workflow
  type-checks:
    uses: ./.github/workflows/reusable.yaml
    with:
      name: rhysd
      # ERROR: Cannot assign bool value to number input
      id: true
      # ERROR: Cannot assign null to string input. If you want to pass string "null", use ${{ 'null' }}
      message: null
    secrets:
      password: p@ssw0rd
permissions: {}
```

Output:

<!-- Skip update output -->

```console
test.yaml:6:11: input "name" is required by "./.github/workflows/reusable.yaml" reusable workflow [workflow-call]
  |
6 |     uses: ./.github/workflows/reusable.yaml
  |           ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:6:11: secret "password" is required by "./.github/workflows/reusable.yaml" reusable workflow [workflow-call]
  |
6 |     uses: ./.github/workflows/reusable.yaml
  |           ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:9:7: input "user" is not defined in "./.github/workflows/reusable.yaml" reusable workflow. defined inputs are "id", "message", "name" [workflow-call]
  |
9 |       user: rhysd
  |       ^~~~~
test.yaml:13:7: secret "credentials" is not defined in "./.github/workflows/reusable.yaml" reusable workflow. defined secret is "password" [workflow-call]
   |
13 |       credentials: my-token
   |       ^~~~~~~~~~~~
test.yaml:22:11: input "id" is typed as number by reusable workflow "./.github/workflows/reusable.yaml". bool value cannot be assigned [expression]
   |
22 |       id: true
   |           ^~~~
test.yaml:24:16: input "message" is typed as string by reusable workflow "./.github/workflows/reusable.yaml". null value cannot be assigned [expression]
   |
24 |       message: null
   |                ^~~~
```

<!-- Skip playground link -->

Reusable workflows can define required/optional inputs and secrets. When they are missing or some undefined input is used in a
workflow call, actionlint reports an error.

And reusable workflows must define types of their inputs by `type:` field. Workflow calls pass constants (`input: 42`) or
expressions (`inputs: ${{ ... }}`) to the inputs or secrets. actionlint checks types of values passed to inputs in workflow call.
When a type of input doesn't match to its definition, actionlint reports an error.

Note that this check only works with local or self-repository reusable workflows (starting with `./` or `$/`).

### Check outputs of workflow call in downstream jobs

Example reusable workflow:

```yaml
# .github/workflows/get-build-info.yaml
on:
  workflow_call:
    outputs:
      version:
        value: ${{ outputs.version }}
        description: version of software

jobs:
  test:
    runs-on: ubuntu-latest
    outputs:
      version: ${{ steps.get_version.outputs.version }}
    steps:
      - run: ...
        id: get_version
permissions: {}
```

Example input:

```yaml
on: push

jobs:
  get_build_info:
    uses: ./.github/workflows/get-build-info.yaml
  downstream:
    needs: [get_build_info]
    runs-on: ubuntu-latest
    steps:
      # OK. `version` is defined in the reusable workflow
      - run: echo '${{ needs.get_build_info.outputs.version }}'
      # ERROR: `tag` is not defined in the reusable workflow
      - run: echo '${{ needs.get_build_info.outputs.tag }}'
permissions: {}
```

Output:

<!-- Skip update output -->

```console
test.yaml:13:24: property "tag" is not defined in object type {version: string} [expression]
   |
13 |       - run: echo '${{ needs.get_build_info.outputs.tag }}'
   |                        ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
```

<!-- Skip playground link -->

A workflow call exposes its outputs through the calling job's outputs object. Another job can access them by listing the calling job in its own `needs:`.
The reusable workflow defines the outputs; actionlint uses those definitions to check downstream references.

In this example, `get-build-info.yaml` defines one output, `version`.
actionlint gives the calling job's outputs the type `{version: string}`.
It then reports the downstream job's reference to the undefined output `tag`.

Note that this check only works with local or self-repository reusable workflows (starting with `./` or `$/`).

### Check permissions of workflow call

Example reusable workflow:

```yaml
# .github/workflows/reusable.yaml
on:
  workflow_call:

jobs:
  snapshot:
    runs-on: ubuntu-latest
    permissions:
      pull-requests: write
    steps:
      - run: ...
```

Example input:

```yaml
on:
  push:
    branches: [main]

jobs:
  # ERROR: The calling job does not grant "pull-requests: write", which the called job requires
  caller:
    uses: ./.github/workflows/reusable.yaml
permissions: {}
```

Output:

<!-- Skip update output -->

```console
test.yaml:8:11: nested job "snapshot" of "./.github/workflows/reusable.yaml" requires "pull-requests: write" but the calling job grants "pull-requests: none" [workflow-call]
  |
8 |     uses: ./.github/workflows/reusable.yaml
  |           ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
```

<!-- Skip playground link -->

A reusable workflow can only use the permissions its caller passes on. GitHub rejects the whole run before any job starts
when a job of the called workflow asks for more, so actionlint compares each job of the called workflow against what the
calling job grants. The grant is the calling job's `permissions:`, or the caller workflow's `permissions:` when the job has
none. When neither exists the grant depends on the repository's "Workflow permissions" setting, which actionlint cannot
read from a workflow file; [`repository.default-workflow-permissions`](config.md#configuration-file) selects which
setting to assume.

Note that this check only works with local or self-repository reusable workflows (starting with `./` or `$/`).

<a id="id-naming-convention"></a>

## ID naming convention

Example input:

```yaml
on: push

jobs:
  # ERROR: '.' cannot be contained in ID
  foo-v1.2.3:
    runs-on: ubuntu-latest
    steps:
      - run: echo 'job ID with version'
        # ERROR: ID cannot contain spaces
        id: echo for test
  # ERROR: ID cannot start with '-'
  -hello-world-:
    runs-on: ubuntu-latest
    steps:
      - run: echo 'oops'
  # ERROR: ID cannot start with numbers
  2d-game:
    runs-on: ubuntu-latest
    steps:
      - run: echo 'oops'
permissions: {}
```

Output:

```console
test.yaml:5:3: invalid job ID "foo-v1.2.3". job ID must start with a letter or _ and contain only alphanumeric characters, -, or _ [id]
  |
5 |   foo-v1.2.3:
  |   ^~~~~~~~~~~
test.yaml:10:13: invalid step ID "echo for test". step ID must start with a letter or _ and contain only alphanumeric characters, -, or _ [id]
   |
10 |         id: echo for test
   |             ^~~~
test.yaml:12:3: invalid job ID "-hello-world-". job ID must start with a letter or _ and contain only alphanumeric characters, -, or _ [id]
   |
12 |   -hello-world-:
   |   ^~~~~~~~~~~~~~
test.yaml:17:3: invalid job ID "2d-game". job ID must start with a letter or _ and contain only alphanumeric characters, -, or _ [id]
   |
17 |   2d-game:
   |   ^~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNqkzjGugzAQBNCeU0xHtUif37lOk2NAWGIj47W8NhRR7h458Q0oV/s0MxIMYlHbdZvMajpgFaHjbxiH/3oBqQSlyspcQi7kp8yavy/NHPWnAKrSgB9W0G8y437D6bLFwUmdhL45wC2NrZLQwsiy90KnJL/QhWKRqLVpXOg57Xw5KXLandb9avB6fwYA/mRanw==)

IDs must start with a letter or `_` and contain only alphanumeric characters, `-` or `_`. actionlint checks the naming
convention, and reports invalid IDs as errors.

<a id="ctx-spfunc-availability"></a>

## Availability of contexts and special functions

Example input:

```yaml
on: push

defaults:
  run:
    # ERROR: No context is available here
    shell: ${{ env.SHELL }}

jobs:
  test:
    strategy:
      matrix:
        directory:
          # OK: 'github' context is available here
          - ${{ github.workflow }}
          # ERROR: 'runner' context is not available here
          - ${{ runner.temp }}
    runs-on: ubuntu-latest
    defaults:
      run:
        # OK: 'env' context is available here
        shell: ${{ env.SHELL }}
    env:
      # ERROR: 'env' context is not available here
      FOO: ${{ env.BAR }}
    steps:
      - env:
          # OK: 'env' context is available here
          FOO: ${{ env.BAR }}
        # ERROR: No context is available here
        shell: ${{ env.SHELL}}
        # ERROR: 'success()' function is not available here
        run: echo 'Success? ${{ success() }}'
        # OK: 'success()' function is available here
        if: success()
permissions: {}
```

Output:

```console
test.yaml:6:16: unavailable-context: context "env" is not allowed here. no context is available here. see https://docs.github.com/en/actions/learn-github-actions/contexts#context-availability for more details [context-availability]
  |
6 |     shell: ${{ env.SHELL }}
  |                ^~~
test.yaml:16:17: unavailable-context: context "runner" is not allowed here. available contexts are "github", "inputs", "needs", "vars". see https://docs.github.com/en/actions/learn-github-actions/contexts#context-availability for more details [context-availability]
   |
16 |           - ${{ runner.temp }}
   |                 ^~~~~~
test.yaml:24:16: unavailable-context: context "env" is not allowed here. available contexts are "github", "inputs", "matrix", "needs", "secrets", "strategy", "vars". see https://docs.github.com/en/actions/learn-github-actions/contexts#context-availability for more details [context-availability]
   |
24 |       FOO: ${{ env.BAR }}
   |                ^~~
test.yaml:30:20: unavailable-context: context "env" is not allowed here. no context is available here. see https://docs.github.com/en/actions/learn-github-actions/contexts#context-availability for more details [context-availability]
   |
30 |         shell: ${{ env.SHELL}}
   |                    ^~~
test.yaml:32:33: unavailable-function: calling function "success" is not allowed here. "success" is only available in "jobs.<job_id>.if", "jobs.<job_id>.steps.if", "jobs.<job_id>.snapshot.if". see https://docs.github.com/en/actions/learn-github-actions/contexts#context-availability for more details [context-availability]
   |
32 |         run: echo 'Success? ${{ success() }}'
   |                                 ^~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNp8j8Fq8zAQhO96ijn8kL8H+wF8KS209BAINE9gO+tYrS0Z7W7TYPTuRU4cm0J6EqP5dmfWuwKDcmvMgZpSO+HCAEFdegBuqesK/BtHkPvK928v2y1iNObDVxMpxHJFJZRCx/NFAX0pwX7PCjjYQLX4cF6+gGxafbTSapWffPhsOn9KAb+RoM5RyIX6YbaDOs5Sf63UiWZdmcpM1vqWK7qk3rsJQPqYwdfdboGen95nhIWG2+JsPfHX1L3glZ1KgurWY7PXuibmx4nli/j/gBg3N9o2xeKYgUJvma13XGCMPwMAgnyD4A==)

GitHub limits which contexts each workflow key can use. For example, `jobs.<job_id>.env` cannot use the `env` context,
but `jobs.<job_id>.steps.env` can.

GitHub also limits where some functions can be called. The status functions `success()`, `failure()`,
`always()` and `cancelled()` can only be used in `if:` conditions. These functions have location restrictions:

- `hashFiles()`
- `always()`
- `success()`
- `failure()`
- `cancelled()`

[The official contexts document][availability-doc] describes which contexts and special functions are available at which workflow
keys.

actionlint reports an error when a context or function is not available at the key where it is used.

<a id="#check-deprecated-workflow-commands"></a>

## Check deprecated workflow commands

Example input:

```yaml
on: push

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      # ERROR: 'set-output' workflow command was deprecated
      - run: echo '::set-output name=foo::bar'
      # OK: Use this instead
      - run: echo "foo=bar" >> "$GITHUB_OUTPUT"
      # OK: 'debug' command is not deprecated
      - run: echo "::debug::Set the Octocat variable"
permissions: {}
```

Output:

```console
test.yaml:8:22: workflow command "set-output" was deprecated. use `echo "{name}={value}" >> "${GITHUB_OUTPUT}"` instead: https://docs.github.com/en/actions/using-workflows/workflow-commands-for-github-actions [deprecated-commands]
  |
8 |       - run: echo '::set-output name=foo::bar'
  |                      ^~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNpsyjFOAzEQRuHep/hlIaXyBUZKChqgChK7NbKXCbso8VieGRrE3ZFJu9Ur3ieV0FzXEL6kKAXAWG0U6F41DeDFq3m65vH+lxo3vSsgDUngZRUciJQtiVtzQ803Pl5EiEruhx0eLyLHknvE6YT48PQyPc+P7+d5ep2nuOeJPrj4J9EbG2xlnBeTJRu+c99yuXIMjfttU92kKuHn928A7dBFAA==)

GitHub deprecated the following workflow commands.

- [`set-output`][deprecate-set-output-save-state]
- [`save-state`][deprecate-set-output-save-state]
- [`set-env`][deprecate-set-env-add-path]
- [`add-path`][deprecate-set-env-add-path]

actionlint detects these commands are used in `run:` and reports them as errors suggesting alternatives. See
[the official document][workflow-commands-doc] for the comprehensive list of workflow commands to know the usage.

<a id="if-cond-constant"></a>

## Constant conditions at `if:`

Example input:

```yaml
on: push

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "DEBUG! ${{ github.event_name }}"
        # ERROR: It is always evaluated to false
        if: false
      - run: echo 'Commit is pushed'
        # OK
        if: ${{ github.event_name == 'push' }}
      - run: echo 'Commit is pushed'
        # OK
        if: |
          github.event_name == 'push'
      - run: echo 'Commit is pushed'
        # ERROR: It is always evaluated to true
        if: |
          ${{ github.event_name == 'push' }}
      - run: echo 'Commit is pushed'
        # ERROR: It is always evaluated to true
        if: "${{ github.event_name == 'push' }} "
      - run: echo 'Commit is pushed to main'
        # OK
        if: github.event_name == 'push' && github.ref_name == 'main'
      - run: echo 'Commit is pushed to main'
        # ERROR: It is always evaluated to true
        if: ${{ github.event_name == 'push' }} && ${{ github.ref_name == 'main' }}
permissions: {}
```

Output:

```console
test.yaml:9:13: constant expression "false" in condition is always falsy. this step or job will be skipped [if-cond]
  |
9 |         if: false
  |             ^~~~~
test.yaml:19:13: if: condition "${{ github.event_name == 'push' }}\n" is always evaluated to true because extra characters are around ${{ }} [if-cond]
   |
19 |         if: |
   |             ^
test.yaml:23:13: if: condition "${{ github.event_name == 'push' }} " is always evaluated to true because extra characters are around ${{ }} [if-cond]
   |
23 |         if: "${{ github.event_name == 'push' }} "
   |             ^~~~
test.yaml:29:13: if: condition "${{ github.event_name == 'push' }} && ${{ github.ref_name == 'main' }}" is always evaluated to true because extra characters are around ${{ }} [if-cond]
   |
29 |         if: ${{ github.event_name == 'push' }} && ${{ github.ref_name == 'main' }}
   |             ^~~
```

[Playground](https://kjanat.github.io/actionlint/#eNq0kM1OxCAUhfd9iiMxsKoPQDIbf+ITuDZUby1mgKb34qby7obxbyZOatW4IuR897sHUrQYMw9N85g6tg0gxFJPYMqR2wrkLkfJ7dbVbBex0MivFNBW0oLuhgR1eXV+c32C03nGg5chd2f0RFFuowuEUtTbDOB7i95tmY5YzEUKwQs878rRvTkYOy7fbGAqbVDKz53PHzcsuf8m/o/i6nsp1BotJCE4Hw/1S2at3+OJ+s9wX/KLjSveo/U+9XV5/ciRpuCZfYpsMZeXAQB4Pty4)

actionlint reports conditions whose outcome it can prove is constant. Removing `if: true`
still requires the default `success()` check. Removing an always-false condition would enable
the work it skips, so the diagnostic does not suggest removing false conditions.
Blank and YAML-null conditions use `success()`. A sole string
literal expression is folded and parsed as condition source. See
[expression behavior](expression-behavior.md#condition-source-is-different-from-a-computed-string) for computed strings.

When an `if:` value contains only a `${{ }}` expression that produces a boolean, the boolean determines the condition.
Characters outside the expression turn the condition into a nonempty string, which evaluates to `true`.

A literal block such as `if: |` adds newline characters outside the expression, making the condition evaluate to `true`.

```yaml
if: |
  ${{ false }}
```

is equivalent to

```yaml
if: "${{ false }}\n"
```

An expression written directly in `if:` ignores surrounding whitespace. The following condition therefore works as intended:

```yaml
if: |
  false
```

actionlint also checks extra characters around `${{ }}` in `if:` which unexpectedly make the conditions true.

<a id="action-metadata-syntax"></a>

## Action metadata syntax validation

Example action metadata:

```yaml
# .github/actions/my-invalid-action/action.yml

name: "My action"
author: "..."
# ERROR: 'description' section is required

branding:
  # ERROR: Invalid icon name
  icon: dog
  # ERROR: Unsupported icon color
  color: gray-white

runs:
  # ERROR: Node.js runtime version is too old
  using: "node16"
  # ERROR: The source file being run by this action does not exist
  main: "this-file-does-not-exist.js"
  # ERROR: 'env' configuration is only allowed for Docker actions
  env:
    SOME_VAR: SOME_VALUE
```

Example input:

```yaml
on: push

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      # actionlint checks an action when it is actually used in a workflow
      - uses: ./.github/actions/my-invalid-action
permissions: {}
```

Output:

<!-- Skip update output -->

```console
test.yaml:8:15: description is required in metadata of "My action" action at "/path/to/repo/.github/actions/my-invalid-action/action.yml" [action]
  |
8 |       - uses: ./.github/actions/my-invalid-action
  |               ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:8:15: incorrect icon name "dog" at branding.icon in metadata of "My action" action at "/path/to/repo/.github/actions/my-invalid-action/action.yml". see the official document to know the exhaustive list of supported icons: https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions#brandingicon [action]
  |
8 |       - uses: ./.github/actions/my-invalid-action
  |               ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:8:15: incorrect color "gray-white" at branding.icon in metadata of "My action" action at "/path/to/repo/.github/actions/my-invalid-action/action.yml". see the official document to know the exhaustive list of supported colors: https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions#brandingcolor [action]
  |
8 |       - uses: ./.github/actions/my-invalid-action
  |               ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:8:15: runtime "node16" is no longer bundled with current GitHub Actions runners. update runs.using in local action "My action" to a current runtime [action]
  |
8 |       - uses: ./.github/actions/my-invalid-action
  |               ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:8:15: file "this-file-does-not-exist.js" does not exist in "/path/to/repo/.github/actions/my-invalid-action". it is specified at "main" key in "runs" section in "My action" action [action]
  |
8 |       - uses: ./.github/actions/my-invalid-action
  |               ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:8:15: "env" is not allowed in "runs" section because "My action" is a JavaScript action. the action is defined at "/path/to/repo/.github/actions/my-invalid-action" [action]
  |
8 |       - uses: ./.github/actions/my-invalid-action
  |               ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
```

<!-- Skip playground link -->

Action metadata files are named `action.yml` or `action.yaml`. Their syntax is defined in [the official document][action-metadata-doc]. A local Dockerfile-only action has no manifest schema to check.

actionlint checks the following in metadata files used by workflows:

- `name:`, `description:`, `runs:` sections are required
- Runner name at `using:` is `composite`, `docker`, or a JavaScript runtime accepted by GitHub's manifest parser.
  The generated runtime table currently includes `node12`, `node16`, `node20`, and `node24`. Accepted metadata syntax
  is separate from lifecycle: `node12` and `node16` are no longer bundled, `node20` is deprecated, and `node24` is current.
  GitHub.com now executes JavaScript actions with Node 24, including legacy `node20` declarations, and no longer
  honors the Node 20 opt-out. This hosted-service policy does not change the generated runner bundle inventory.
  `node22` is a valid Node.js release but is not accepted
  as an action's `runs.using` value. The check does not infer action runtime support from Node.js release numbers.
- Missing or unexpected keys under `runs:`. The required and allowed keys depend on whether the action uses Docker,
  composite steps or JavaScript. For example, Docker actions require `image:`.
- Invalid composite steps. A script step needs `run:` and `shell:`, and an action step needs `uses:`.
- Missing files referenced under `runs:`, such as the JavaScript entry point at `main:`.
- An unsupported icon name at `branding.icon`. See [the supported icons][branding-icons-doc].
- An unsupported icon color at `branding.color`. Supported colors are white, yellow, blue, green, orange, red,
  purple and gray-dark.

actionlint checks action metadata used by workflows. You can also pass `action.yml` or `action.yaml` directly on the command line.

For a Composite action, each item in `steps:` must be a mapping in one of the two shapes the runner accepts: a script step
with `run:` and `shell:` keys, or an action step with `uses:` key. actionlint reports a step which mixes the two shapes, misses
`shell:` next to `run:`, has a key the runner does not know such as `parallel:` or `timeout-minutes:`, has a non-string value
at `run:`, `shell:`, or `uses:`, has an empty `uses:` value, is `null`, or calls a reusable workflow at `uses:`.
`working-directory:` is only allowed in a script step and `with:` is only allowed in an action step.

Inside a Composite action's `steps:`, actionlint checks expression contexts at
`if:`, `run:`, `working-directory:`, `name:`, `shell:`, `continue-on-error:`, `with:`,
and `env:`, including expressions supplying a whole `with:` or `env:` mapping.
The `secrets`, `vars`, and `needs` contexts are not available to a
composite action and are reported as errors. `secrets` and `vars` values must be
passed to the action as `inputs:` instead.
An `if:` expression is also checked without the optional `${{ }}` wrapper. Delimiters inside quoted expression strings
are treated as literal text.

Input `default:` expressions in JavaScript, Docker, and Composite actions have a narrower set of contexts:
`github`, `job`, `matrix`, `runner`, and `strategy`, as defined in the runner's
[`action_yaml.json`](https://github.com/actions/runner/blob/main/src/Runner.Worker/action_yaml.json).
References to `inputs`, `env`, `steps`, `secrets`, `vars`, or `needs` in a default are reported
at the value in the action metadata file.

The step keys, expression contexts, and special-function availability come from the runner's template schema.
`hashFiles()` accepts 1 to 255 arguments in input defaults and composite step expressions.
The status functions `always()`, `cancelled()`, `failure()`, and `success()` are available in composite `if:` conditions
and accept no arguments. Using one in a step name or input default is reported at that value.
These checks do not perform full expression type checking or apply every workflow step rule inside the action.
The schema-derived data ships with actionlint; linting does not download the schema.

<a id="deprecated-inputs-usage"></a>

## Deprecated inputs usage

When the host supplies action metadata declaring `fail_on_error` deprecated:

```yaml
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: reviewdog/action-actionlint@v1
        with:
          # ERROR: Using a deprecated input
          fail_on_error: true
permissions: {}
```

Expected diagnostic:

```console
test.yaml:9:11: deprecated-input: avoid using deprecated input "fail_on_error" in action "reviewdog/action-actionlint@v1": Deprecated, use `fail_level` instead [action]
  |
9 |           fail_on_error: true
  |           ^~~~~~~~~~~~~~
```

Action authors can deprecate inputs with [`deprecationMessage`][dep-msg]. actionlint reports deprecated inputs used in a
workflow as errors.

actionlint also checks local actions. In addition to the usage of deprecated inputs, it checks the input definitions in
the action metadata `action.yml` or `action.yaml`.

Example action metadata:

```yaml
# .github/actions/my-action/action.yml

name: "My action"
author: "..."
description: "..."

inputs:
  new-input:
  # This input is deprecated.
  old-input:
    deprecationMessage: This input is deprecated. Use new-input instead.
  # ERROR: Empty deprecation message is not allowed
  empty-message:
    deprecationMessage:

runs:
  ...
```

Example input:

```yaml
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/my-action
        with:
          # ERROR: Using a deprecated input
          old-input: some value
permissions: {}
```

Output:

<!-- Skip update output -->

```console
test.yaml:6:15: input "empty-message" is deprecated but "deprecationMessage" is empty in metadata of "My action" action at "/path/to/.github/actions/my-action/action.yaml" [action]
  |
6 |       - uses: ./.github/actions/my-action
  |               ^~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:9:11: avoid using deprecated input "old-input" in action "My action" defined at "./.github/actions/my-action": This input is deprecated. Use new-input instead [action]
  |
9 |           old-input: some value
  |           ^~~~~~~~~~
```

<!-- Skip playground link -->

Note that the usage of deprecated inputs marked as 'required' are not reported as error because it is not possible to avoid using
them.

<a id="yaml-anchors"></a>

## YAML anchors

GitHub Actions [supports][anochor-support-announce] YAML [anchor and alias nodes][yaml-anchor-spec]. actionlint checks them in
workflows.

Anchor and alias names containing `+`, such as `&git+opts` and `*git+opts`, are rejected by GitHub Actions and reported as errors.

If an alias supplies the wrong value type, actionlint reports the error at that alias use and includes the anchor's location.
For example, `- &Runner { uses: example/action@v1 }` defines a complete step mapping. Reusing it as `- *Runner` is valid, but
`- uses: *Runner` is a type error because `uses` requires a scalar string. Each incorrect use gets its own diagnostic.

actionlint detects errors under YAML anchors. When an alias node references an erroneous anchor, actionlint checks them as if the
alias node is replaced with the anchor node. This means that one anchor node may be checked multiple times and actionlint may
report multiple similar errors at the same source location.

Example input:

```yaml
on: push

jobs:
  test:
    services:
      nginx:
        image: nginx:latest
        credentials: &credentials # ERROR: Credentials are embedded directly in workflow
          username: my-user-name
          password: P@ssw0rd
          # ERROR: Unexpected key 'email'
          email: me@example.com
      redis:
        image: redis:latest
        credentials: *credentials
    runs-on: ubuntu-latest
    steps:
      - run: ./do_something.sh
permissions: {}
```

Output:

```console
test.yaml:10:21: "password" section in "nginx" service should be specified via secrets. do not put password value directly [credentials]
   |
10 |           password: P@ssw0rd
   |                     ^~~~~~~~
test.yaml:10:21: "password" section in "redis" service should be specified via secrets. do not put password value directly [credentials]
   |
10 |           password: P@ssw0rd
   |                     ^~~~~~~~
test.yaml:12:11: unexpected key "email" for "credentials" section. expected one of "password", "username" [syntax-check]
   |
12 |           email: me@example.com
   |           ^~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNp8kM1K/DAUxfd9igN/+C+Ejq6zGhDXSl9A0ubaXs1HuTexM4jvLnHGEhBchd/5SDhJ0WAtunTdaxrVdEAmzfUElOSdJ9ILAXHmePoBgIOdyVxVb2tv9yYhRzGz9WrwvyH8w8MwPA4G941ohUBhJOfIwbHQlP0ZHLEleXvxadsvBoqSRBvIIJz7Cn2lJrBa1S2JM3g6qm534hqTgmVvEOhIJxtWT4cphasv5Fh/7buof+27aeg7ISVqX3+2jCXm0jdlzbTub/Q1aXC4delZU6C8cJwPunQrSWBVTlENPj6/BgCwH4EC)

actionlint also checks how anchors and aliases are used. In the following example, it reports recursive aliases and unused
anchors as errors.

Example input:

```yaml
on: push

jobs:
  test:
    services:
      nginx:
        image: nginx:latest
        credentials: &credentials
          username: ${{ secrets.user }}
          password: ${{ secrets.password }}
    runs-on: ubuntu-latest
    steps:
      - run: ./download.sh
        # OK: Valid alias to &credentials
        env: *credentials
      - run: ./upload.sh
        # ERROR: Unused anchor 'credentials'
        env: &credentials
      - &recursive
        run: ./some_script.sh
        # ERROR: Recursively referencing the anchor
        env: *recursive
permissions: {}
```

Output:

```console
test.yaml:18:14: anchor "credentials" is defined but not used [syntax-check]
   |
18 |         env: &credentials
   |              ^~~~~~~~~~~~
test.yaml:18:14: expecting a single ${{...}} expression or mapping value for "env" section, but found plain text node [syntax-check]
   |
18 |         env: &credentials
   |              ^~~~~~~~~~~~
test.yaml:22:14: "env" section is alias node but mapping node is expected [syntax-check]
   |
22 |         env: *recursive
   |              ^~~~~~~~~~
test.yaml:22:14: recursive alias "recursive" is found. anchor was declared at line:19, column:9 [syntax-check]
   |
22 |         env: *recursive
   |              ^~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNpsj8FugzAQRO98xRwqDpWgd/9M5cAqcQVra8cmlRD/XqEGlyo5oRne8+5GdUiFt6b5ihe6BsjCvH8Bii1hEP4mQK9Bv48AhNlfxT3aye9e/TeYjKI5+IkO7SlVAigUUz+Lw9u6gjKYZPZ7i207ccmT92jjf+5oD9aKstuvKZeiuXSnhZgl1SO6nXToP8Z41yn6seetDhNdHN6f161SSS+V9pXSmgzFGBap8OMVxlk+OVhI+Xn6n5XE5kCGqHRYt58BAMm8f2c=)

actionlint reports dangling aliases at the alias use when no matching anchor has been defined.

Example input:

```yaml
on: push

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: ./download.sh
        # ERROR: &credentials is not defined
        env: *credentials
permissions: {}
```

Output:

```console
test.yaml:9:14: could not parse as YAML: unknown anchor 'credentials' referenced [syntax-check]
  |
9 |         env: *credentials
  |              ^~~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNosyjEOAiEQheGeU7zahLXnNqxMshgcCG9GC+PdDbrVX/xf14ThPEK4950pACa0VWC6Mi7gu6t5bHm936LJ4F8BccmE7Vr6S1vPZeNxLkD0mXC5TSmiVnNjGDIflaxdmfD+fAcA/4ooPw==)

---

[Installation](install.md) | [Usage](usage.md) | [Configuration](config.md) | [Integration interfaces](api.md) | [References](reference.md)

[yamllint]: https://github.com/adrienverge/yamllint
[syntax-doc]: https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions
[filter-pattern-doc]: https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#filter-pattern-cheat-sheet
[shellcheck]: https://github.com/koalaman/shellcheck
[shellcheck-install]: https://github.com/koalaman/shellcheck#installing
[SC1091]: https://github.com/koalaman/shellcheck/wiki/SC1091
[SC2050]: https://github.com/koalaman/shellcheck/wiki/SC2050
[SC2194]: https://github.com/koalaman/shellcheck/wiki/SC2194
[SC2154]: https://github.com/koalaman/shellcheck/wiki/SC2154
[SC2153]: https://github.com/koalaman/shellcheck/wiki/SC2153
[SC2157]: https://github.com/koalaman/shellcheck/wiki/SC2157
[SC2043]: https://github.com/koalaman/shellcheck/wiki/SC2043
[shellcheck-env-var]: https://github.com/koalaman/shellcheck/wiki/Integration#environment-variables
[expr-doc]: https://docs.github.com/en/actions/learn-github-actions/expressions
[contexts-doc]: https://docs.github.com/en/actions/learn-github-actions/contexts
[funcs-doc]: https://docs.github.com/en/actions/learn-github-actions/expressions#functions
[needs-doc]: https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idneeds
[needs-context-doc]: https://docs.github.com/en/actions/learn-github-actions/contexts#needs-context
[shell-doc]: https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#using-a-specific-shell
[matrix-doc]: https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idstrategymatrix
[webhook-doc]: https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#webhook-events
[schedule-event-doc]: https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#scheduled-events
[cron-syntax]: https://pubs.opengroup.org/onlinepubs/9699919799/utilities/crontab.html#tag_20_25_07
[schedule-item-doc]: https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#onschedule
[gh-hosted-runner]: https://docs.github.com/en/actions/using-github-hosted-runners/about-github-hosted-runners
[self-hosted-runner]: https://docs.github.com/en/actions/hosting-your-own-runners/about-self-hosted-runners
[action-uses-doc]: https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idstepsuses
[credentials-doc]: https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idcontainercredentials
[actions-cache]: https://github.com/actions/cache
[permissions-doc]: https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#permissions
[perm-config-doc]: https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#permissions
[generate-webhook-events]: https://github.com/kjanat/actionlint/tree/HEAD/internal/development/generators/event-triggers
[security-doc]: https://docs.github.com/en/actions/reference/security/secure-use
[reusable-workflow-doc]: https://docs.github.com/en/actions/learn-github-actions/reusing-workflows
[create-reusable-workflow-doc]: https://docs.github.com/en/actions/learn-github-actions/reusing-workflows#creating-a-reusable-workflow
[reusable-workflow-call-keys]: https://docs.github.com/en/actions/learn-github-actions/reusing-workflows#supported-keywords-for-jobs-that-call-a-reusable-workflow
[object-filter-syntax]: https://docs.github.com/en/actions/learn-github-actions/expressions#object-filters
[github-script]: https://github.com/actions/github-script
[workflow-dispatch-event]: https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow_dispatch
[workflow-dispatch-input-type-announce]: https://github.blog/changelog/2021-11-10-github-actions-input-types-for-manual-workflows/
[reusable-workflow-outputs]: https://docs.github.com/en/actions/using-workflows/reusing-workflows#using-outputs-from-a-reusable-workflow
[inherit-secrets-announce]: https://github.blog/changelog/2022-05-03-github-actions-simplify-using-secrets-with-reusable-workflows/
[specific-paths-doc]: https://docs.github.com/en/actions/using-workflows/triggering-a-workflow#using-filters-to-target-specific-paths-for-pull-request-or-push-events
[availability-doc]: https://docs.github.com/en/actions/writing-workflows/choosing-what-your-workflow-does/accessing-contextual-information-about-workflow-runs#context-availability
[deprecate-set-output-save-state]: https://github.blog/changelog/2022-10-11-github-actions-deprecating-save-state-and-set-output-commands/
[deprecate-set-env-add-path]: https://github.blog/changelog/2020-10-01-github-actions-deprecating-set-env-and-add-path-commands/
[workflow-commands-doc]: https://docs.github.com/en/actions/using-workflows/workflow-commands-for-github-actions
[action-metadata-doc]: https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions
[branding-icons-doc]: https://github.com/github/docs/blob/main/content/actions/creating-actions/metadata-syntax-for-github-actions.md#exhaustive-list-of-all-currently-supported-icons
[operators-doc]: https://docs.github.com/en/actions/learn-github-actions/expressions#operators
[dep-msg]: https://docs.github.com/en/actions/reference/workflows-and-actions/metadata-syntax#inputsinput_iddeprecationmessage
[anochor-support-announce]: https://github.blog/changelog/2025-09-18-actions-yaml-anchors-and-non-public-workflow-templates/
[yaml-anchor-spec]: https://yaml.org/spec/1.2.2/#71-alias-nodes
