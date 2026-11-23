# Check examples

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
test.yaml:7:21: truncated: "max-parallel" value 1.5 is truncated to 1 [numeric-values]
  |
7 |       max-parallel: 1.5
  |                     ^~~
test.yaml:12:26: expecting a single ${{...}} expression or float number literal, but found plain text node [syntax-check]
   |
12 |         timeout-minutes: two minutes
   |                          ^~~
```

[Playground](https://kjanat.github.io/actionlint/#eNo0zF2KQjEMxfH3u4qzgQ53BuYlu8mFVCv9oklQEfcu1foUzp/wa5XQXc/bpR1KG2CiNi+gNtjkdP8sIHLKIbIaocW4YuFb6Dw4Z8mE35//dx9eNUzaD6/mIfNklypdv2SYnwTNIh1/+74yYKlIcwslVTdRgl0b1ti6jJJUU6tKeDxfAwD1zTt+)

<a id="check-syntax-expression"></a>

## Syntax check for expression `${{ }}`

Example input:

```yaml
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo '${{ "hello" }}'
      - run: echo '${{ 1 + 1 }}'
      - run: echo "${{ toJson(hashFiles('**/lock', '**/cache/') }}"
      - run: echo '${{ github.event. }}'
permissions: {}
```

Output:

```console
test.yaml:6:24: invalid-syntax: got unexpected character '"' while lexing expression, expecting 'a'..'z', 'A'..'Z', '_', '0'..'9', ''', '}', '(', ')', '[', ']', '.', '!', '<', '>', '=', '&', '|', '*', ',', ' '. do you mean string literals? only single quotes are available for string delimiter [expression-syntax]
  |
6 |       - run: echo '${{ "hello" }}'
  |                        ^
test.yaml:7:26: invalid-syntax: got unexpected character '+' while lexing expression, expecting 'a'..'z', 'A'..'Z', '_', '0'..'9', ''', '}', '(', ')', '[', ']', '.', '!', '<', '>', '=', '&', '|', '*', ',', ' ' [expression-syntax]
  |
7 |       - run: echo '${{ 1 + 1 }}'
  |                          ^
test.yaml:8:65: invalid-syntax: unexpected end of input while parsing arguments of function call. expecting ",", ")" [expression-syntax]
  |
8 |       - run: echo "${{ toJson(hashFiles('**/lock', '**/cache/') }}"
  |                                                                 ^~
test.yaml:9:38: invalid-syntax: unexpected end of input while parsing object property dereference like 'a.b' or array element dereference like 'a.*'. expecting "IDENT", "*" [expression-syntax]
  |
9 |       - run: echo '${{ github.event. }}'
  |                                      ^~
```

[Playground](https://kjanat.github.io/actionlint/#eNp0zUGqgzAQxvG9p/gID+KzVXGbA3TRW2gYmrRpRpykG8ndS+za1Qz8f/BxNFizuObJi5gGSCSpXmDLUfra85Jjyn2YazuSJFrlp4C+SgOyjqH/9h3KUQisUIo+IxMumE6AqiDxXTi2bhZ384Gk1V03BrYvfUV97WwdjfofpaizkYdPLi8DfSim4RhbaXt7Ec9RDPbyHQA94UpL)

<a id="check-type-check-expression"></a>

## Type checks for expression syntax in `${{ }}`

actionlint uses these expression types:

| Notation                 | Type                                     |
| ------------------------ | ---------------------------------------- |
| `any`                    | Any value, used when typing fails        |
| `number`                 | Integer or float                         |
| `bool`                   | Boolean                                  |
| `string`                 | String                                   |
| `null`                   | `null`                                   |
| `array<T>`               | Array of `T`                             |
| `object`                 | Object with any properties of type `any` |
| `{prop1: T1, prop2: T2}` | Object with typed properties             |
| `{string => T}`          | Object with values of `T`, like `env`    |

actionlint reports an object or array in a string template. The runner prints it as `Object` or `Array`.

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

An expression can supply a whole object, for example at `env:`.

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

Use `${{ insert }}` as a mapping key to merge a mapping or an object expression into `env:`:

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

`${{ insert }}` also works in action `with:` inputs and in `matrix:`:

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

<a id="check-contexts-and-builtin-func"></a>

## Contexts and built-in functions

Example input:

```yaml
on: [push, issues]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo '${{ unknown_context }}'
      - run: echo '${{ github.events }}'
      - run: echo "${{ startWith('hello, world', 'lo,') }}"
      - run: echo "${{ startsWith('hello, world') }}"
      - run: echo "${{ startsWith('hello, world', github.event) }}"
      - run: echo "${{ contains('hello, world', 'lo,') }}"
      - run: echo "${{ contains(github.event.issue.labels.*.name, 'enhancement') }}"
      - if: contains(fromJSON('["push", "pull_request"]'), github.event_name)
        run: echo 'Push or pull request'
permissions: {}
```

Output:

```console
test.yaml:6:24: unknown-variable: undefined variable "unknown_context". available variables are "env", "github", "inputs", "job", "matrix", "needs", "runner", "secrets", "steps", "strategy", "vars" [expression-reference]
  |
6 |       - run: echo '${{ unknown_context }}'
  |                        ^~~~~~~~~~~~~~~
test.yaml:7:24: unknown-property: property "events" is not defined in github; did you mean "event"? [expression-reference]
  |
7 |       - run: echo '${{ github.events }}'
  |                        ^~~~~~~~~~~~~
test.yaml:8:24: unknown-function: undefined function "startWith"; did you mean "startsWith"? [expression-reference]
  |
8 |       - run: echo "${{ startWith('hello, world', 'lo,') }}"
  |                        ^~~~~~~~~
test.yaml:9:24: argument-count: number of arguments is wrong. function "startsWith(string, string) -> bool" takes 2 parameters but 1 arguments are given [expression-call]
  |
9 |       - run: echo "${{ startsWith('hello, world') }}"
  |                        ^~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNqckUFO6zAQhvc9xch6ktsnJwfIEVgAEgsWVRU5YYoNzjh4xhQpyt2RSwUtol10lcV83z8T/5EaWI+ZnQHPnJE3i5fYcbMAEGQpX4CUiatC5i6T5CrYMtuPWHDkLwqgKmQD2LsI+t80QaZXijtq+0iCHwLzrM+hz15c7mp8RxI+A6oCstgkj17cUjsMIRrYxRSetAEdotErmGd10eU/5Ossc3L1xYzyAtYTX3H0t3q8rN63VQfbYeD6f012QAMayVnqcUCSX6F+2/wkbVMcbh7ubpd6rUr5yoAacwhtwreMLGqjV6c/15YFq0MaHJd3n9lBTFB8OPh6MWIaPLOPxA1M8+cAkFHBYQ==)

Context and function names are case-insensitive. actionlint checks `format()` placeholders and `fromJSON()` strings, and
types a `fromJSON()` result from its JSON.

Example input:

```yaml
on: [push, issues]

jobs:
  test:
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
test.yaml:5:18: unknown-property: property "mac" is not defined in this object [expression-reference]
  |
5 |     runs-on: ${{ fromJSON('{"win":"windows-latest","linux":"ubuntul-latest"}')['mac'] }}
  |                  ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:8:24: format-placeholder: format string "{0}{1}" does not contain placeholder {2}. remove argument which is unused in the format string [expression-call]
  |
8 |       - run: echo "${{ format('{0}{1}', 1, 2, 3) }}"
  |                        ^~~~~~
test.yaml:8:24: unknown-script-value: interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution [script-interpolation]
  |
8 |       - run: echo "${{ format('{0}{1}', 1, 2, 3) }}"
  |                        ^~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:10:24: format-placeholder: format string "{0}{1}{2}" contains placeholder {2} but only 2 arguments are given to format [expression-call]
   |
10 |       - run: echo "${{ format('{0}{1}{2}', 1, 2) }}"
   |                        ^~~~~~
test.yaml:13:31: invalid-json: broken JSON string is passed to fromJSON() at offset 23: expected ',' or ']' after JSON array value [expression-call]
   |
13 |         if: contains(fromJson('["main","release","dev"'), github.ref_name)
   |                               ^~~~~~~~~~~~~~~~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNqMj8FKAzEQhu/7FL+DkF1IxdZbHsGDHvRWimS3qRvZnSyZxAoh7y5b7VHwMnOY/5uPP7DBfskyaniR7OTQNB+hF9MAyUlaNxAzy2aN3paCUwzz48vzU6sKnT2TWecxnGUz2RUhTZPn/EWGcp855el6qKrbq9kO6oBaL48luUV+HMBm9Ri4YQygiynE2aZWlftatlVpbDV2Gg8daqX/QWV35f6CXkcv8AILWdzg7YQ+Wh7Gm98s4E8GQ+BkPUt7aS+BW7Wn2XomTdFNzoojTUf3SarTePdpzP1ddKc3trPrmsXF2Yv4wGJQ6vcAwFpyyA==)

<a id="check-contextual-step-object"></a>

## Contextual typing for `steps.<step_id>` objects

Example input:

```yaml
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    outputs:
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
test.yaml:9:24: unknown-property: property "get_value" is not defined in steps [expression-reference]
  |
9 |       - run: echo '${{ steps.get_value.outputs.name }}'
  |                        ^~~~~~~~~~~~~~~
test.yaml:14:24: unknown-script-value: interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution [script-interpolation]
   |
14 |       - run: echo '${{ steps.get_value.outputs.name }}'
   |                        ^~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:21:24: unknown-property: property "get_value" is not defined in steps [expression-reference]
   |
21 |       - run: echo '${{ steps.get_value.outputs.name }}'
   |                        ^~~~~~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNqskDFLxEAQhfv7FY/l4KrkByx4hY1aaZHURy5OTCTZWTIzNiH/XXaNVgEFr5rifd88eBw8okl/eOer+AOgJJouMFuQIuV2taBWjE3KcsSm0VS+OKBj9nDHZYEoRSnfSC8fzWhUbmAZmomwri4LGfp2i9TjQW3POP3hxWnHcx3zXaYdzme448NT9VjfX57r6qWu3GYAw6vHz+Mb9u96LYd2NBk4bBZrT/Ov0/5znEjzNEhqFY9l/RwAbpWYcw==)

actionlint checks output names of composite actions. It accepts any output name of JavaScript and Docker actions.

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
      - run: echo ${{ steps.cache.outputs.cache-hit }}
      - run: echo ${{ steps.cache.outputs.cache_hit }}
permissions: {}
```

Output:

```console
test.yaml:8:23: unknown-property: property "cache" is not defined in steps [expression-reference]
  |
8 |       - run: echo ${{ steps.cache.outputs.cache-hit }}
  |                       ^~~~~~~~~~~
test.yaml:15:23: unknown-script-value: interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution [script-interpolation]
   |
15 |       - run: echo ${{ steps.cache.outputs.cache-hit }}
   |                       ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:16:23: unknown-script-value: interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution [script-interpolation]
   |
16 |       - run: echo ${{ steps.cache.outputs.cache_hit }}
   |                       ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNqkj8FKxTAQRff9irsQnhba7lzMypW/IXlxcGJrE97MKFL675K21K1gNmHmnMtN8kwortI07/mq1ADGavUGbj5rVwW/+mzeTaGyDalx0d0CumoSOErG3bLssI8hCvfZrbgdUyfJsK5nzJWVEKKlPOuwKU+fjwcG0ithW56br2RC5wSM/E1bpQSV5zSx3l/admj7Kcfx8vDbVU8JJoR+KCGO4Y31v6//Y+zliBW+fSTV+lXCsv4MAIvrcvY=)

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
      - run: echo '${{ matrix.platform }}'
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
      - run: echo '${{ matrix.os }}'
permissions: {}
```

Output:

```console
test.yaml:18:24: unknown-property: property "platform" is not defined in matrix [expression-reference]
   |
18 |       - run: echo '${{ matrix.platform }}'
   |                        ^~~~~~~~~~~~~~~
test.yaml:19:24: unknown-property: property "dev" is not defined in matrix.package [expression-reference]
   |
19 |       - run: echo '${{ matrix.package.dev }}'
   |                        ^~~~~~~~~~~~~~~~~~
test.yaml:22:25: unknown-script-value: interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution [script-interpolation]
   |
22 |           echo 'os: ${{ matrix.os }}'
   |                         ^~~~~~~~~
test.yaml:23:35: unknown-script-value: interpolated scalar is not proved to occupy a script data argument; pass it through an environment variable and read that variable as data instead [script-interpolation]
   |
23 |           echo 'node version: ${{ matrix.node }}'
   |                                   ^~~~~~~~~~~
test.yaml:24:30: unknown-script-value: interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution [script-interpolation]
   |
24 |           echo 'package: ${{ matrix.package.name }} (optional=${{ matrix.package.optional }})'
   |                              ^~~~~~~~~~~~~~~~~~~
test.yaml:24:67: unknown-script-value: interpolated scalar is not proved to occupy a script data argument; pass it through an environment variable and read that variable as data instead [script-interpolation]
   |
24 |           echo 'package: ${{ matrix.package.name }} (optional=${{ matrix.package.optional }})'
   |                                                                   ^~~~~~~~~~~~~~~~~~~~~~~
test.yaml:31:24: unknown-property: property "os" is not defined in matrix [expression-reference]
   |
31 |       - run: echo '${{ matrix.os }}'
   |                        ^~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNqMUu1q4zAQ/J+nGMKBE4gNOWICgnuScD8UW07U2pLQSklLqncviq06Hy7tL7GrmdXsjLRiMJ6Osxe9JzYDnCAXT4Cc5U4c3vsK6Liz8i1VgCaGnd975Xze8shb4SxVrc801P+/sErXgmG33qywLse24dUrP4hxJpBD8U4wzBut5zd9QBsnteItg7NeTFH23H5HaXhLI0eqqvX147tXjevyboIyHcO2KIvNtW29ojx69udyGQwpNCGEwTFhKA3NI5hBVEeN7AZuWu4abTuEkP0E7e0panF6Rn/c6OyZMZBHXdkTKq6Jk7AkH/a4XkwxUkqYkBadRwhYJKv/TYDSHUJYTu6sTJckQRLIiEo2UtSjFtn0z1daOS4VLZJo062QbYsyW/YpxH/3l92HdfdJf5/UYKARtpMUtRHDJXwOAPu56uI=)

actionlint types each matrix axis from its elements. Mixed element types give `any`:

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

Quoted, `!!str` and block scalars are strings.

```yaml
strategy:
  matrix:
    version:
      # string values
      - "3.10"
      - !!str 3.11
      - >-
        3.12
      - 0b10
    flag:
      # bool values
      - true
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
test.yaml:7:13: invalid value "0b10" for "!!int" tag [syntax-check]
  |
7 |           - !!int 0b10
  |             ^~~~~
test.yaml:10:13: tag of a quoted or block scalar must be "!!str" but got "!!bool" [syntax-check]
   |
10 |           - !!bool "true"
   |             ^~~~~~
test.yaml:13:13: tag of a YAML scalar must be one of "!!str", "!!bool", "!!int", "!!float", "!!null" but got "!!timestamp" [syntax-check]
   |
13 |           - !!timestamp 2026-08-21
   |             ^~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNpcj81OxCAUhfd9ijMTk1lhmC6M4W1ArzMY/sK9GE3Duxu0raYrcg4f5Ds5GZTG9+k9OzYTIMQyToClWqHb128CopXqP7cEfFBln9NfASicTj4JtLvqvX4L9naEXM4BZ6mNzvtNpUCW6fXIio/EYmPBrOcnpZ/VfP1BakusxoDmWpKmgh3yqzsV3j5SgzSgl3vG5WFZ1iWP6wD0jn/t0D1Umxl6v0yFavQ8HrLB0r8HAGKgWqY=)

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
test.yaml:15:24: unknown-property: property "prepare" is not defined in needs [expression-reference]
   |
15 |       - run: echo '${{ needs.prepare.outputs.prepared }}'
   |                        ^~~~~~~~~~~~~
test.yaml:23:45: unknown-script-value: interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution [script-interpolation]
   |
23 |       - run: echo 'build something with ${{ needs.install.outputs.installed }} and ${{ needs.prepare.outputs.prepared }}'
   |                                             ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:23:88: unknown-script-value: interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution [script-interpolation]
   |
23 |       - run: echo 'build something with ${{ needs.install.outputs.installed }} and ${{ needs.prepare.outputs.prepared }}'
   |                                                                                        ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:25:24: unknown-property: property "foo" is not defined in needs.install.outputs [expression-reference]
   |
25 |       - run: echo '${{ needs.install.outputs.foo }}'
   |                        ^~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:27:24: unknown-property: property "some_job" is not defined in needs [expression-reference]
   |
27 |       - run: echo '${{ needs.some_job }}'
   |                        ^~~~~~~~~~~~~~
test.yaml:32:24: unknown-property: property "build" is not defined in needs [expression-reference]
   |
32 |       - run: echo '${{ needs.build.outputs.built }}'
   |                        ^~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNqkkdFq8zAMhe/zFIfyQ2/+5AH8KmOMZNEWl9Qylswugt992HUdtpVtrBeBKJLOd47CzsBHWboTT2I6wDrRcV3zK8BRfVS5FK1Hs8FhGIZD+Ryikz7LxCk6jf06KomWlij5ttznSQN6XhjHqgThM+li3euxA3wgPwa6ia69+8lZJT8f0V/G/m0bHNEsQyUP1dC1npFS3pyiXecLqcwbPNRw/6+mH28Fynt6d5pC36PgzeqC3Xt10ry3H4iUMLoZv475zYE+Q16Yf17Klp9OPNVJ1oWC+fsdduFykeYlV1oYnsLZilh2YrCl9wEAcCTxDw==)

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
  |
7 |       - run: ./bad.sh # ERROR: tracked with Git mode 100644
  |              ^~~~~~~~
```

<!-- Skip playground link -->

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
test.yaml:13:19: SC2086: Double quote to prevent globbing and word splitting [shellcheck]
   |
13 |       - run: echo $FOO
   |                   ^~~~
```

<!-- Skip playground link -->

actionlint replaces each `${{ }}` placeholder with underscores. When it knows every string value of a placeholder, it
runs ShellCheck once per value.

actionlint disables [SC2050], [SC2194], [SC2157], [SC2043] and [SC1091]. When it cannot name every environment variable
of a step, it also disables [SC2153] and [SC2154].

[SC1091]: https://github.com/koalaman/shellcheck/wiki/SC1091
[SC2043]: https://github.com/koalaman/shellcheck/wiki/SC2043
[SC2050]: https://github.com/koalaman/shellcheck/wiki/SC2050
[SC2153]: https://github.com/koalaman/shellcheck/wiki/SC2153
[SC2154]: https://github.com/koalaman/shellcheck/wiki/SC2154
[SC2157]: https://github.com/koalaman/shellcheck/wiki/SC2157
[SC2194]: https://github.com/koalaman/shellcheck/wiki/SC2194

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
          stale-pr-message: ${{ github.event.pull_request.title }} was closed
      - uses: actions/github-script@v9
        with:
          # ERROR: Using the potentially untrusted input can cause script injection
          script: console.log('${{ github.event.head_commit.author.name }}')
      - name: Get comments
        run: echo '${{ toJSON(github.event.*.body) }}'
      - name: Do something with checking skip
        run: if [ "${{ contains(github.event.pull_request.title, '[SKIP]') }}" = "true" ]; then echo "skip"; fi
permissions: {}
```

Output:

```console
test.yaml:10:24: untrusted-interpolation: "github.event.pull_request.title" is potentially untrusted. avoid using it directly in inline scripts. instead, pass it through an environment variable. see https://docs.github.com/en/actions/reference/security/secure-use#good-practices-for-mitigating-script-injection-attacks for more details [script-injection]
   |
10 |         run: echo '${{ github.event.pull_request.title }}'
   |                        ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:18:36: untrusted-interpolation: "github.event.head_commit.author.name" is potentially untrusted. avoid using it directly in inline scripts. instead, pass it through an environment variable. see https://docs.github.com/en/actions/reference/security/secure-use#good-practices-for-mitigating-script-injection-attacks for more details [script-injection]
   |
18 |           script: console.log('${{ github.event.head_commit.author.name }}')
   |                                    ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:20:31: untrusted-interpolation: object filter extracts potentially untrusted properties "github.event.comment.body", "github.event.discussion.body", "github.event.issue.body", "github.event.pull_request.body", "github.event.review.body", "github.event.review_comment.body". avoid using the value directly in inline scripts. instead, pass the value through an environment variable. see https://docs.github.com/en/actions/reference/security/secure-use#good-practices-for-mitigating-script-injection-attacks for more details [script-injection]
   |
20 |         run: echo '${{ toJSON(github.event.*.body) }}'
   |                               ^~~~~~~~~~~~~~~~~~~
test.yaml:22:24: unknown-script-value: interpolated scalar is not proved to occupy a script data argument; pass it through an environment variable and read that variable as data instead [script-interpolation]
   |
22 |         run: if [ "${{ contains(github.event.pull_request.title, '[SKIP]') }}" = "true" ]; then echo "skip"; fi
   |                        ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNqMkUFr20AQhe/6FQ9RsFMitT12QyGHltIGkkByMybI64l3a2lH3Zl1KEb/vawUjN1i2tMyzJv3Pd6GpiODRxItOBgs+tS2T5F+JhK9RJ/ELYviB6/EFICSaH6BmIJU+SCtUtBUtU3ejStR6mVSARUmwH30QZHN8WoO9drSq2w0NCDrGLM3+z02Xl1a1bSjoPVxpno8wzDMDoQkJAaNVc9B3ok2LV3vPrw/WL94deYwAZF6rpS3FAwyS8hGUqkf726+3GIYjqSjWdXHqiORZkMG/xcOL43Atiy0PhNz8qjERt/r9e7j2bSTwsByEG6pbnkz/7siR836yXLXea2bpI5jnYvPPV388RVfSZGVFFTO1a/8/eHudn6CeFuveP3r4qT6yfEzQ7gjdT5sxvywjuw2T7L1/SnEP2OBMkMsB218kPk/+rzEbPFw8+1+OcvwEp9QakxUYnkFdRSm3GVGlVd49kVPsfMiuWeD/fB7ACq26os=)

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

`exclude:` matches only the original rows, before `include:` adds any.

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

| Filters                       | Events                                                                       |
| ----------------------------- | ---------------------------------------------------------------------------- |
| `paths`, `paths-ignore`       | `push`, `pull_request`, `pull_request_target`                                |
| `branches`, `branches-ignore` | `merge_group`, `push`, `pull_request`, `pull_request_target`, `workflow_run` |
| `tags`, `tags-ignore`         | `push`                                                                       |

<a id="check-workflow-dispatch-events"></a>

## Workflow dispatch event validation

Example input:

```yaml
on:
  workflow_dispatch:
    inputs:
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
        default: teen

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      # ERROR: Undefined input
      - run: echo "${{ inputs.massage }}"
      - run: echo "${{ env[inputs.verbose] }}"
      - run: echo "${{ env[inputs.age] }}"
      # ERROR: `github.event.inputs` is also not defined
      - run: echo "${{ github.event.inputs.massage }}"
permissions: {}
```

Output:

```console
test.yaml:5:15: input type of workflow_dispatch event must be one of "string", "number", "boolean", "choice", "environment" but got "text" [syntax-check]
  |
5 |         type: text
  |               ^~~~
test.yaml:7:7: input type of "kind" is "choice" but "options" is not set [events]
  |
7 |       kind:
  |       ^~~~~
test.yaml:15:18: default value "Chobi" of "name" input is not included in its options "\"Tama\", \"Mike\"" [events]
   |
15 |         default: Chobi
   |                  ^~~~~
test.yaml:21:18: type of "verbose" input is "boolean". its default value "yes" must be "true" or "false" [events]
   |
21 |         default: yes
   |                  ^~~
test.yaml:24:18: type of "age" input is "number" but its default value "teen" cannot be parsed as a float number: strconv.ParseFloat: parsing "teen": invalid syntax [events]
   |
24 |         default: teen
   |                  ^~~~
test.yaml:31:24: unknown-property: property "massage" is not defined in inputs; did you mean "message"? [expression-reference]
   |
31 |       - run: echo "${{ inputs.massage }}"
   |                        ^~~~~~~~~~~~~~
test.yaml:32:28: operand-type: property access of object must be type of string but got "bool" [expression-type]
   |
32 |       - run: echo "${{ env[inputs.verbose] }}"
   |                            ^~~~~~~~~~~~~~
test.yaml:33:28: operand-type: property access of object must be type of string but got "number" [expression-type]
   |
33 |       - run: echo "${{ env[inputs.age] }}"
   |                            ^~~~~~~~~~
test.yaml:35:24: unknown-property: property "massage" is not defined in github.event.inputs; did you mean "message"? [event-payload]
   |
35 |       - run: echo "${{ github.event.inputs.massage }}"
   |                        ^~~~~~~~~~~~~~~~~~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNqMkcFugzAMhu88hVXtCg/AdefddpumKQEXMogdxQ5dhXj3CZqtmuiq3qzPnw1/zFQXACeOw3Hk00frJBht+hUCOApJ5VIDuPanAtBzwBoUvzSjwdGu3fTsGsyQjMe7AgAHdUxytQBKeDXe/AEvbriOtHg0adQannu2LmOPIqbbfU00OuoynDBalp1jmUc0tN9/RsnwxmZK3mLcDykiFcUn2y2TouhlMiaSkqmGZBNpKkez9raWKIbfFyhXswZseobD0zzng1TebAFhWQ7/mUjTW7Zz1PcHddPdVzunfbIVTkha3fihgNE7ke2SMC/fAwAu9qwi)

<a id="check-glob-pattern"></a>

## Glob filter pattern syntax validation

Example input:

```yaml
on:
  push:
    branches:
      - "^foo-"
    tags:
      - "v*+"
      - "v[9-1]"
    paths:
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
test.yaml:4:10: character '^' is invalid for branch and tag names. ref name cannot contain spaces, ~, ^, :, [, ?, *. see `man git-check-ref-format` for more details. note that regular expression is unavailable. note: filter pattern syntax is explained at https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#filter-pattern-cheat-sheet [glob]
  |
4 |       - "^foo-"
  |          ^~~~~~
test.yaml:6:12: invalid glob pattern. unexpected character '+' while checking special character + (one or more). the preceding character must not be special character. note: filter pattern syntax is explained at https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#filter-pattern-cheat-sheet [glob]
  |
6 |       - "v*+"
  |            ^~
test.yaml:7:14: invalid glob pattern. unexpected character '1' while checking character range in []. start of range '9' (57) is larger than end of range '1' (49). note: filter pattern syntax is explained at https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#filter-pattern-cheat-sheet [glob]
  |
7 |       - "v[9-1]"
  |              ^~~
test.yaml:9:9: '.' and '..' are not allowed in glob path. note: filter pattern syntax is explained at https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#filter-pattern-cheat-sheet [glob]
  |
9 |       - ./foo/bar.txt
  |         ^~~~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNpMjEEKAjEMRfc9RZil0g4u7VVEoR06dkST0iQiiHeXaTddhZf3+ITeABTlvF+AWAMuOXEnAAvTbSWyU2MJ99G8D8dpoMvZnq79UYLkoXTzSjTHUJ18xJgHxSYlsfSoKrIl9KBRUdQ+w+6aYkllmKqKHtKSCZxzpqT62pg3Qvbw/f0HAFv0NkQ=)

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

<a id="check-runner-labels"></a>

## Runner labels

List self-hosted runner labels and [larger runner][larger-runner] names in `repository.runners` in
[`actionlint.yaml`](config.md). actionlint reports unknown labels only when that list exists. The examples assume
`repository: {runners: []}`.

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
    runs-on: macos-10.13
    steps:
      - run: echo ...
permissions: {}
```

Output:

```console
test.yaml:10:13: label "linux-latest" is unknown. available labels are "windows-latest", "windows-latest-8-cores", "windows-2025", "windows-2025-vs2026", "windows-2022", "windows-11-arm", "windows-11-vs2026-arm", "ubuntu-slim", "ubuntu-latest", "ubuntu-latest-4-cores", "ubuntu-latest-8-cores", "ubuntu-latest-16-cores", "ubuntu-26.04", "ubuntu-26.04-arm", "ubuntu-24.04", "ubuntu-24.04-arm", "ubuntu-22.04", "ubuntu-22.04-arm", "xcode-27", "xcode-27-xlarge", "macos-latest", "macos-latest-xlarge", "macos-latest-large", "macos-26-intel", "macos-26-xlarge", "macos-26-large", "macos-26", "macos-15-intel", "macos-15-xlarge", "macos-15-large", "macos-15", "self-hosted", "x64", "arm", "arm64", "linux", "macos", "windows". if it is a custom label for a self-hosted or larger runner, set list of labels in actionlint.yaml config file [runner-label]
   |
10 |           - linux-latest
   |             ^~~~~~~~~~~~
test.yaml:16:13: label "gpu" is unknown. available labels are "windows-latest", "windows-latest-8-cores", "windows-2025", "windows-2025-vs2026", "windows-2022", "windows-11-arm", "windows-11-vs2026-arm", "ubuntu-slim", "ubuntu-latest", "ubuntu-latest-4-cores", "ubuntu-latest-8-cores", "ubuntu-latest-16-cores", "ubuntu-26.04", "ubuntu-26.04-arm", "ubuntu-24.04", "ubuntu-24.04-arm", "ubuntu-22.04", "ubuntu-22.04-arm", "xcode-27", "xcode-27-xlarge", "macos-latest", "macos-latest-xlarge", "macos-latest-large", "macos-26-intel", "macos-26-xlarge", "macos-26-large", "macos-26", "macos-15-intel", "macos-15-xlarge", "macos-15-large", "macos-15", "self-hosted", "x64", "arm", "arm64", "linux", "macos", "windows". if it is a custom label for a self-hosted or larger runner, set list of labels in actionlint.yaml config file [runner-label]
   |
16 |           - gpu
   |             ^~~
test.yaml:22:14: label "macos-10.13" is unknown. available labels are "windows-latest", "windows-latest-8-cores", "windows-2025", "windows-2025-vs2026", "windows-2022", "windows-11-arm", "windows-11-vs2026-arm", "ubuntu-slim", "ubuntu-latest", "ubuntu-latest-4-cores", "ubuntu-latest-8-cores", "ubuntu-latest-16-cores", "ubuntu-26.04", "ubuntu-26.04-arm", "ubuntu-24.04", "ubuntu-24.04-arm", "ubuntu-22.04", "ubuntu-22.04-arm", "xcode-27", "xcode-27-xlarge", "macos-latest", "macos-latest-xlarge", "macos-latest-large", "macos-26-intel", "macos-26-xlarge", "macos-26-large", "macos-26", "macos-15-intel", "macos-15-xlarge", "macos-15-large", "macos-15", "self-hosted", "x64", "arm", "arm64", "linux", "macos", "windows". if it is a custom label for a self-hosted or larger runner, set list of labels in actionlint.yaml config file [runner-label]
   |
22 |     runs-on: macos-10.13
   |              ^~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNqEj81qxCAUhfc+xVl0OUqnHWbhq5QubHqbWOIPXoWUkHcvoqEIhVnJ8d57+L7gNWLhRXyHD9YCyMS5vgDnZDLNPy0BzuRktzMBqXhP6S8DEs5MgeVqasswWK0v23+DN6b1Sy6BM31e2toF2/32PmyZ5O634WeORXQKltXiad87ompkOI7uQZFPTFkPNGhaApRSohu/6LGseVyf1fX1cUek5CyzDZ419uN3AMzLWyg=)

actionlint reports conflicting labels in one `runs-on:` list.

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
      - uses: .github/my-actions/do-something
permissions: { contents: read }
```

Output:

```console
test.yaml:7:15: invalid-reference: specifying action "actions/checkout" in invalid format because ref is missing. available formats are "{owner}/{repo}@{ref}", "{owner}/{repo}/{path}@{ref}", "./{path}", or "$/{path}" [invalid-action-reference]
  |
7 |       - uses: actions/checkout
  |               ^~~~~~~~~~~~~~~~
test.yaml:9:15: invalid-reference: specifying action "checkout@v2" in invalid format because owner is missing. available formats are "{owner}/{repo}@{ref}", "{owner}/{repo}/{path}@{ref}", "./{path}", or "$/{path}" [invalid-action-reference]
  |
9 |       - uses: checkout@v2
  |               ^~~~~~~~~~~
test.yaml:11:15: invalid-reference: tag of Docker action should not be empty: "docker://image" [invalid-action-reference]
   |
11 |       - uses: "docker://image:"
   |               ^~~~~~~~~~~~~~~~~
test.yaml:12:15: invalid-reference: specifying action ".github/my-actions/do-something" in invalid format because ref is missing. available formats are "{owner}/{repo}@{ref}", "{owner}/{repo}/{path}@{ref}", "./{path}", or "$/{path}" [invalid-action-reference]
   |
12 |       - uses: .github/my-actions/do-something
   |               ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNpczcGKwzAMBNB7vkLk7jXsUaf9FccRsTe1FCypUEr/vbgll5wG5g2MMMLhWqZ/WRQnACO1kQDdWcNwX5zNwy0N+5AaHfpdAQRwJUVI2aqwxlwo7+J24bP+u/9eZF4l79QxxtrSRjhf/GerVnyJ7RHOj1WCSiMrlbfpoN6q6gCEJ2RhIzZF6JRWeL0HAO2eR9o=)

<a id="check-local-action-inputs"></a>

## Local action inputs validation at `with:`

Action metadata at `.github/actions/my-action/action.yaml`:

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
      - uses: ./.github/actions/my-action
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
test.yaml:6:15: missing input "message" which is required by action "My action" defined at "./.github/actions/my-action". all required inputs are "message" [required-input]
  |
6 |       - uses: ./.github/actions/my-action
  |               ^~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:11:11: input "additions" is not defined in action "My action" defined at "./.github/actions/my-action". available inputs are "addition", "message", "name" [unknown-input]
   |
11 |           additions: foo, bar
   |           ^~~~~~~~~~
```

<!-- Skip playground link -->

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
        shell: fish
    steps:
      - run: echo 'hello'
        # OK: Custom shell
        shell: "perl {0}"
  windows:
    runs-on: windows-latest
    steps:
      - run: echo 'hello'
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
test.yaml:16:16: shell name "fish" is invalid. available names are "bash", "pwsh", "python", "sh" [shell-name]
   |
16 |         shell: fish
   |                ^~~~
test.yaml:25:16: shell name "sh" is invalid on Windows. available names are "bash", "cmd", "powershell", "pwsh", "python" [shell-name]
   |
25 |         shell: sh
   |                ^~
```

[Playground](https://kjanat.github.io/actionlint/#eNqkkEGKhTAQRPc5ReHGlTDr3CZqSzLEdEincUC8+xBHho+rj39XST2SR3GyyCrefPMo1gAxJP1pASiaZGiAjpqqDtFVknpWUinLHwUMjbSgyTN6TzFyfzWAtLPF7MS/T2feqJzZAKubbjqrm1hebWZanMb6L9Q+uL+5BPGP1LtMJWL/OjoDbCHNvMlN6Lr9ZKCH82QqaxAJnMRiP34HAFUzhLI=)

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

<a id="check-duplicate-workflow-names"></a>

## Workflow name uniqueness

Example input:

```yaml
name: Publish packages
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
permissions: {}
```

Output:

<!-- Skip update output -->

```console
.github/workflows/release-go.yml:1:7: workflow name "Publish packages" is also used by .github/workflows/publish-crates.yml; the Actions tab cannot tell these workflows apart [duplicate-workflow-name]
  |
1 | name: Publish packages
  |       ^~~~~~~
```

<!-- Skip playground link -->

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

<a id="permissions"></a>

## Permissions

Example input:

```yaml
on: push

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
test.yaml:3:14: "write" is invalid for permission for all the scopes. available values are "read-all", "write-all" or {} [permissions]
  |
3 | permissions: write
  |              ^~~~~
test.yaml:10:7: unknown permission scope "check". all available permission scopes are "actions", "artifact-metadata", "attestations", "checks", "code-quality", "contents", "copilot-requests", "deployments", "discussions", "drives", "id-token", "issues", "models", "packages", "pages", "pull-requests", "repository-projects", "security-events", "statuses", "vulnerability-alerts" [permissions]
   |
10 |       check: write
   |       ^~~~~~
test.yaml:12:15: "readable" is invalid as permission of scope "issues". available values are "read", "write", "none" [permissions]
   |
12 |       issues: readable
   |               ^~~~~~~~
test.yaml:14:15: "write" is invalid as permission of scope "models". available values are "read", "none" [permissions]
   |
14 |       models: write
   |               ^~~~~
test.yaml:16:29: "write" is invalid as permission of scope "vulnerability-alerts". available values are "read", "none" [permissions]
   |
16 |       vulnerability-alerts: write
   |                             ^~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNpUjtGtwyAMRf+ZwguwANtAYgnecwD52q26fUWqVM2XpXOvjz16oumoIUzWowFtdCR6ajMO4W8UpEBkDFuTSL0jriUv3s2j5JWd0a/gBERb5e3/sn1QA5yRSDnvuciFj7Gz4F59uHTWXJo0e8UsrHZrwHh+T8X1WiLe6qDKIuMdAAD//6u/RE0=)

<a id="check-reusable-workflows"></a>

## Reusable workflows

actionlint checks calls of local reusable workflows, referenced with `./` or `$/`, against the called workflow.

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
test.yaml:24:18: input "path" of workflow_call event has the default value "", but it is also required. if an input is marked as required, its default value will never be used [events]
   |
24 |         default: ""
   |                  ^~
test.yaml:30:24: unknown-script-value: interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution [script-interpolation]
   |
30 |       - run: echo "${{ inputs.scheme }}://${{ inputs.host }}:${{ inputs.port }}${{ inputs.path }}"
   |                        ^~~~~~~~~~~~~
test.yaml:30:47: unknown-script-value: interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution [script-interpolation]
   |
30 |       - run: echo "${{ inputs.scheme }}://${{ inputs.host }}:${{ inputs.port }}${{ inputs.path }}"
   |                                               ^~~~~~~~~~~
test.yaml:30:84: unknown-script-value: interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution [script-interpolation]
   |
30 |       - run: echo "${{ inputs.scheme }}://${{ inputs.host }}:${{ inputs.port }}${{ inputs.path }}"
   |                                                                                    ^~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNp8kU1uAjEMhfecwhp1C6g/q5yhi/6o62ommCY0EwfbEUUod6/CABpRprvks/3y4kfRzAB2xN/rQLtP24ZQAYCPKasMZwCxDns83wBWKJZ9Uk/RwPuxCLSGj7fnUcu6zUENONUkF6z7hAZE2cevE3Qkav7O4U/bp4ALS/1/04lYJ4y9EOukrcbcPzw+NVfSMfcd8gluM/J+Qvu11q7FBw3qNmj1bK9VN2WvVXetwLjNnnFlQDnjDdfN7V1sqDuGtSJzbOAcZV4fyV2OmuehVZTBkyimS7Dz2mkArSNo7g6HU+6LIXAoxSyXI1yjqnCE6v6hlDGp/yqlmSXk3ot4imLgUH4HAL68tMQ=)

### Check workflow call syntax

Example input:

```yaml
on: push
jobs:
  job1:
    uses: owner/repo/path/to/workflow.yml@v1
    runs-on: ubuntu-latest
  job2:
    # ERROR: Local file path with ref is not available
    uses: ./.github/workflows/ci.yml@main
  job3:
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
test.yaml:5:5: when a reusable workflow is called with "uses", "runs-on" is not available. only following keys are allowed: "name", "uses", "with", "secrets", "needs", "if", "permissions", "cache-mode", "strategy", and "concurrency" in job "job1" [syntax-check]
  |
5 |     runs-on: ubuntu-latest
  |     ^~~~~~~~
test.yaml:8:11: reusable workflow call "./.github/workflows/ci.yml@main" at "uses" is not following the format "owner/repo/path/to/workflow.yml@ref", "./path/to/workflow.yml", nor "$/path/to/workflow.yml". see https://docs.github.com/en/actions/learn-github-actions/reusing-workflows for more details [workflow-call]
  |
8 |     uses: ./.github/workflows/ci.yml@main
  |           ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:10:5: "with" is only available for a reusable workflow call with "uses" but "uses" is not found in job "job3" [syntax-check]
   |
10 |     with:
   |     ^~~~~
test.yaml:17:11: could not read reusable workflow file for "./.github/workflows/not-existing.yml": open /path/to/repo/.github/workflows/not-existing.yml: no such file or directory [workflow-call]
   |
17 |     uses: ./.github/workflows/not-existing.yml
   |           ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
```

[Playground](https://kjanat.github.io/actionlint/#eNqEzkFORCEMBuD9O0UvwCOjrlh5FZh0ho68ltAiGuPdDUM0rnQFi///+gsHqF3zdpOkYQO4STrNF6AragAZjM03rOJrtOxN/JD2ciky9vejPL+e7uHWWd3Eeups3ZVoqLa8h9/e7vcrWe7ph1F/pjt1ROLVeFyNQZbXD+AiEiDF9vc1ADWs+l1yMxkAz1kgYymy+Kf/BrGYwzdSI77OaVvFdpAqCWuAj8+vAQBshmLg)

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

### Check inputs and secrets in workflow call

Example reusable workflow:

```yaml
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
      - run: echo hello
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

### Check outputs of workflow call in downstream jobs

Example reusable workflow:

```yaml
on:
  workflow_call:
    outputs:
      version:
        value: ${{ jobs.test.outputs.version }}
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
      - run: echo '${{ needs.get_build_info.outputs.version }}'
      # ERROR: `tag` is not defined in the reusable workflow
      - run: echo '${{ needs.get_build_info.outputs.tag }}'
permissions: {}
```

Output:

<!-- Skip update output -->

```console
test.yaml:10:24: unknown-script-value: interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution [script-interpolation]
   |
10 |       - run: echo '${{ needs.get_build_info.outputs.version }}'
   |                        ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:12:24: unknown-property: property "tag" is not defined in needs.get_build_info.outputs [expression-reference]
   |
12 |       - run: echo '${{ needs.get_build_info.outputs.tag }}'
   |                        ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
```

<!-- Skip playground link -->

### Check permissions of workflow call

Example reusable workflow:

```yaml
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

GitHub rejects the whole run when a called job needs more permissions than the caller grants. When the caller has no
`permissions:`, set [`repository.default-workflow-permissions`](config.md#configuration-file) to the repository's
default.

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

<a id="check-deprecated-workflow-commands"></a>

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

Characters outside `${{ }}` in `if:` turn the condition into a nonempty string, which is `true`. `if: |` adds a newline:

```yaml
if: |
  ${{ false }}
```

A bare expression ignores surrounding whitespace:

```yaml
if: |
  false
```

<a id="action-metadata-syntax"></a>

## Action metadata syntax validation

Example action metadata:

```yaml
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
      - uses: ./.github/actions/my-invalid-action
permissions: {}
```

Output:

<!-- Skip update output -->

```console
.github/actions/my-invalid-action/action.yml:15:3: file "this-file-does-not-exist.js" does not exist in "/path/to/repo/.github/actions/my-invalid-action". it is specified at "main" key in "runs" section in "My action" action [missing-file]
   |
15 |   main: "this-file-does-not-exist.js"
   |   ^~~~~
test.yaml:7:15: unexpected-field: "env" is not allowed in "runs" section because "My action" is a JavaScript action. the action is defined at "/path/to/repo/.github/actions/my-invalid-action" [syntax-check]
  |
7 |       - uses: ./.github/actions/my-invalid-action
  |               ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:7:15: missing-field: description is required in metadata of "My action" action at "/path/to/repo/.github/actions/my-invalid-action/action.yml" [syntax-check]
  |
7 |       - uses: ./.github/actions/my-invalid-action
  |               ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:7:15: invalid-branding-color: incorrect color "gray-white" at branding.color in metadata of "My action" action at "/path/to/repo/.github/actions/my-invalid-action/action.yml". see the official document to know the exhaustive list of supported colors: https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions#brandingcolor [action-branding]
  |
7 |       - uses: ./.github/actions/my-invalid-action
  |               ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:7:15: invalid-branding-icon: incorrect icon name "dog" at branding.icon in metadata of "My action" action at "/path/to/repo/.github/actions/my-invalid-action/action.yml". see the official document to know the exhaustive list of supported icons: https://docs.github.com/en/actions/creating-actions/metadata-syntax-for-github-actions#brandingicon [action-branding]
  |
7 |       - uses: ./.github/actions/my-invalid-action
  |               ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:7:15: runtime-deprecated: runtime "node16" is deprecated; current GitHub Actions runners use "node24" instead. update runs.using in local action "My action" to a current runtime [action-runtime]
  |
7 |       - uses: ./.github/actions/my-invalid-action
  |               ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
```

<!-- Skip playground link -->

<a id="deprecated-inputs-usage"></a>

## Deprecated inputs usage

Example action metadata:

```yaml
name: "My action"
author: "..."
description: "..."

inputs:
  new-input:
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
test.yaml:6:15: empty-deprecation-message: input "empty-message" is deprecated but "deprecationMessage" is empty in metadata of "My action" action at "/path/to/repo/.github/actions/my-action/action.yaml" [action-input-deprecation]
  |
6 |       - uses: ./.github/actions/my-action
  |               ^~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:9:11: avoid using deprecated input "old-input" in action "My action" defined at "./.github/actions/my-action": This input is deprecated. Use new-input instead [deprecated-input]
  |
9 |           old-input: some value
  |           ^~~~~~~~~~
```

<!-- Skip playground link -->

<a id="yaml-anchors"></a>

## YAML anchors

actionlint checks each alias as a copy of its anchor and can report one error several times at the anchor.

Example input:

```yaml
on: push

jobs:
  test:
    services:
      nginx:
        image: nginx:latest
        credentials: &credentials
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

[Playground](https://kjanat.github.io/actionlint/#eNp8kM9KxDAQxu99ijl5EFo9z2kfwTeQbDO0I51JyJe4K+K7S9i1FAVP4ff9IR+TnCk3rMPwls7ggagKan+JIOVdZ8GNiHxRv/4AkVpYhO/qFnpv9+YiUbxq2MD0cKA9QdQgxYMJk32MHcZOh0AOwCWVyPRyAi7PJR5MsaAbk8lJrsHyJtOc7O4XiYo/Q2/qf0Mffw8tzTH2E7Vz89rGQxlV8v7H2JNM01NMr0gmdVVfJqxDlmIKaHIwfX59DwBCfG2Z)

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

[shellcheck]: https://github.com/koalaman/shellcheck
[larger-runner]: https://docs.github.com/en/actions/how-tos/manage-runners/larger-runners/manage-larger-runners
