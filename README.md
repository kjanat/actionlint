# actionlint

[![CI Status][ci-badge]][ci]
[![Latest release][release-badge]][releases]
[![NPM Version][npm-badge]][npm-package]
[![PyPI version][pypi-badge]][pypi-package]
[![pkg.pr.new][preview-badge]][previews]
[![Docker latest][docker-badge]][docker-latest]
[![Sponsor this project][sponsor-badge]][sponsor]

[![UPCOMING: actionlint v2. Try the master preview.](docs/assets/brand/upcoming-v2.svg)](#get-actionlint)

actionlint checks GitHub Actions workflows, action manifests and the scripts inside them.

[![Watch actionlint check workflows, scripts, and action metadata](docs/assets/actionlint-v2.gif)](docs/assets/actionlint-v2.mp4?raw=true)

[Try the playground](https://kjanat.github.io/actionlint) · [Install](#get-actionlint) · [Browse the rules](docs/rules.md)

## One command checks workflows, actions, and scripts

```sh
actionlint check
```

actionlint discovers workflows and action manifests in a Git repository and follows local action and reusable-workflow dependencies.

| What you wrote                                    | What actionlint checks                                              |
| ------------------------------------------------- | ------------------------------------------------------------------- |
| `${{ matrix.platform }}`                          | Whether `platform` exists and has the expected type.                |
| `needs: [build]`                                  | Whether the job exists and whether the dependencies form a cycle.   |
| `uses: ./actions/deploy`                          | Required inputs, declared outputs and composite steps.              |
| A reusable-workflow call                          | Inputs, secrets, outputs and permission constraints.                |
| `run: ...`                                        | ShellCheck findings for shell and Ruff findings for Python.         |
| `${{ github.event.issue.title }}` inside a script | Whether untrusted text reaches executable script text.              |
| `runs-on`, trigger filters, or a schedule         | Runner compatibility, glob syntax, cron expressions and time zones. |

## See the mistake before you push it

Save this workflow as `.github/workflows/ci.yml` and run `actionlint check --output-format=pretty .github/workflows/ci.yml`:

<!-- BEGIN generated demo -->

```yaml
name: CI
on: push
permissions: {}
jobs:
  test:
    strategy:
      matrix:
        os: [ubuntu-latest, windows-latest]
    runs-on: ${{ matrix.platform }}
    steps:
      - run: echo "Testing"
```

```console
error[expression-reference] unknown-property
  property "platform" is not defined in matrix
  --> .github/workflows/ci.yml:9:18
  |
9 |     runs-on: ${{ matrix.platform }}
  |                  ^^^^^^^^^^^^^^^
  = config: lint.rules.correctness.expression-reference
```

<!-- END generated demo -->

## GitHub Actions features GitHub doesn't even know about

Build an action's inputs or `env:` entries with `${{ insert }}`:

```yaml
with:
  ${{ insert }}: >-
    ${{ fromJSON('{"fetch-depth": 0, "persist-credentials": false}') }}
```

actionlint checks the inserted names and values.

## Use the weird parts of GitHub Actions

GitHub accepts comments inside JSON, unquoted keys, trailing text after the closing brace and mixed-case function names. actionlint understands them. The [actions-rs CI workflow](https://github.com/kjanat/actions-rs/blob/master/.github/workflows/ci.yml) uses all of them:

```yaml
runs-on: ${{ matrix.os }}
strategy:
  matrix: |-
    ${{
      FrOmJsOn(
        tOjSoN(
          fRoMjSoN(
            '{
              // load-bearing newline
              os:
              [
                ''ubuntu-latest'',
                "windows-latest"
                ,''macos-latest'',
              ],
              octal: [010], hex: ["0xFF"]
              /*,array_hole: [,]*/ , empty_string: [""]
              ,null_value: [null],undefined_value: ["undefined"],
              not_a_number: ["NaN"], positive_infinity: ["Infinity"]
              ,constructor: ["new Date()"],
            }
            this trailing sentence is apparently fine'
          )
        )
      )
    }}
```

actionlint resolves the matrix and reports the loose JSON as information. In the same workflow it catches `if: ${{ format('{0}', false) }}`. That expression yields the string `"false"`, and the job runs.

The [expression behavior guide](docs/expression-behavior.md) explains why `'017' == 17` and `fromJSON('017') == 15` can both be true.

## ShellCheck and Ruff belong in the same report

Initialize a project once:

```sh
actionlint config init --toml
actionlint check
```

`config init` installs the [ShellCheck](https://github.com/kjanat/shellcheck-rs) and Ruff WebAssembly plugins and pins their versions in `.github/actionlint.toml`. See [script-linter configuration](docs/config.md#ruff).

## Preview the fix, then apply it

```sh
actionlint fix
actionlint fix --apply
```

The first command previews safe edits as JSON. The second applies them. Add `--include-suggested` to review edits that need a judgment call.

## Get diagnostics and fixes while you edit

Run the language server over standard input and output:

```sh
actionlint lsp
```

The server checks unsaved buffers and offers quick fixes, suppressions and expression completion. See [editor support](docs/lsp.md). The [browser playground](https://kjanat.github.io/actionlint) runs the same server.

## Check the actions your workflow calls

actionlint fetches remote action and reusable-workflow metadata to check inputs, outputs, secrets and permissions. Warm the cache before working offline:

```sh
actionlint cache warm .github/workflows
actionlint check --offline .github/workflows
```

See [remote dependency checks](docs/remote-actions.md).

## Configure rules in TOML, with editor completion

Add rule settings to `.github/actionlint.toml`:

```toml
#:schema https://cdn.jsdelivr.net/gh/kjanat/actionlint@2/actionlint.schema.json

[lint.rules]
preset                            = "recommended"
security                          = "error"
suspicious.mixed-type-comparisons = "error"
conventions.job-timeout           = { level = "error", options = { max-minutes = 30 } }

[[overrides]]
includes                                   = [".github/workflows/legacy.yml"]
lint.rules.correctness.deprecated-commands = "off"
```

[Tombi](https://github.com/tombi-toml/tombi) reads the schema directive for [validation](https://tombi-toml.github.io/tombi/docs/json-schema/) and [completion](https://tombi-toml.github.io/tombi/docs/language-server/completion/).

```sh
actionlint rules
actionlint rules matrix
actionlint check --strict
```

YAML, JSON and JSONC also work. `extends` shares local or HTTPS configuration files.

Inspect the effective settings and their sources:

```sh
actionlint config show --origin
actionlint config validate
actionlint doctor
```

Migrate an existing configuration:

```sh
actionlint config migrate
actionlint config migrate --write
```

See [configuration](docs/config.md), the [rule reference](docs/rules.md) and the [migration guide](docs/config-migration.md).

## Add your own checks

WebAssembly plugins can analyze documents, check embedded languages or provide runner inventory.

```sh
actionlint plugin search shellcheck
actionlint plugin list
actionlint plugin sync
actionlint plugin update
```

`sync` restores the pinned versions. `update` resolves newer releases. The [plugin guide](docs/plugins.md) has SDKs and examples.

## Send results where you work

```sh
actionlint check --json
actionlint check --output-format=jsonl
actionlint check --output-format=github
actionlint check --output-format=sarif --output-file=actionlint.sarif
```

See [result formats](docs/results.md) before you build a report consumer.

The [GitHub Action](docs/action.md) adds annotations, job summaries, SARIF output and pull-request reviews. The [integration guide](docs/usage.md#tools-integration) covers pre-commit, Git hooks, reviewdog and editors.

## Get actionlint

See the [installation guide](docs/install.md). Try the latest preview from `master`:

```sh
npx https://pkg.pr.new/kjanat/actionlint/@kjanat/actionlint@master
```

Or build from source with Go 1.27 or newer:

```sh
go install ./cmd/actionlint
```

Add Go's installation directory to `PATH` and keep `git` available. See [usage](docs/usage.md) and the [v2 migration notes](docs/releases/v2.0.0.md).

## Build and contribute

Read the [contributing guide](CONTRIBUTING.md), then use the project tools through mise:

```sh
mise exec -- runner install
mise exec -- go build ./cmd/actionlint
mise exec -- run test
mise exec -- run fmt
```

## License

actionlint uses the [MIT license](LICENSE.txt). The [identity artwork](docs/assets/brand/LICENSE.md) is by Kaj Kowalski and licensed under CC BY 4.0.

[docker-badge]: https://img.shields.io/badge/Docker-latest-black?logo=docker&logoColor=white&labelColor=2496ED
[docker-latest]: https://hub.docker.com/r/kjanat/actionlint/tags?name=latest
[ci-badge]: https://github.com/kjanat/actionlint/actions/workflows/ci.yml/badge.svg
[ci]: https://github.com/kjanat/actionlint/actions/workflows/ci.yml
[npm-badge]: https://img.shields.io/npm/v/@kjanat/actionlint?logo=npm&labelColor=CB3837&color=black
[npm-package]: https://npm.im/@kjanat/actionlint
[preview-badge]: https://pkg.pr.new/badge/kjanat/actionlint
[previews]: https://pkg.pr.new/~/kjanat/actionlint
[release-badge]: https://img.shields.io/github/v/release/kjanat/actionlint?logo=github&logoColor=white&label=release&labelColor=181717&color=black
[releases]: https://github.com/kjanat/actionlint/releases
[sponsor-badge]: https://img.shields.io/badge/Sponsor-ea4aaa?logo=githubsponsors&logoColor=white
[sponsor]: https://github.com/sponsors/kjanat

[pypi-badge]: https://img.shields.io/pypi/v/actionlint-kjanat?logo=python&logoColor=white&labelColor=3776AB&color=black
[pypi-package]: https://pypi.org/project/actionlint-kjanat/
