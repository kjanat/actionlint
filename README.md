# actionlint

[![CI Status][ci-badge]][ci]
[![Latest release][release-badge]][releases]
[![NPM Version][npm-badge]][npm-package]
[![PyPI version][pypi-badge]][pypi-package]
[![pkg.pr.new][preview-badge]][previews]
[![Docker latest][docker-badge]][docker-latest]
[![Docker nightly][docker-nightly-badge]][docker-nightly]
[![Sponsor this project][sponsor-badge]][sponsor]

[![UPCOMING — actionlint v2. Try the master preview.](docs/assets/brand/upcoming-v2.svg)](#get-actionlint)

**Catch the broken workflow before the runner does.**

actionlint checks GitHub Actions workflows, action manifests, and the scripts inside them. Find the misspelled matrix property, the missing action input, the unsafe interpolation, and the shell bug before the next push.

[![Watch actionlint check workflows, scripts, and action metadata](docs/assets/actionlint-v2.gif)](docs/assets/actionlint-v2.mp4?raw=true)

[Try the playground](https://kjanat.github.io/actionlint) · [Install](#get-actionlint) · [Browse the rules](docs/rules.md) · [Watch the video](docs/assets/actionlint-v2.mp4?raw=true)

v2 brings Cargo-style diagnostics, WebAssembly plugins, ShellCheck and Ruff, action discovery, a language server, source fixes, and structured reports into the same checker.

## One command checks workflows, actions, and scripts

```sh
actionlint check
```

Run it in a Git repository. actionlint discovers workflows and `action.yml` or `action.yaml` manifests, including local actions that no workflow references. It follows local action and reusable-workflow dependencies and checks scripts inside composite actions.

| What you wrote                                    | What actionlint checks                                                         |
| ------------------------------------------------- | ------------------------------------------------------------------------------ |
| `${{ matrix.platform }}`                          | Whether `platform` exists and its value has the expected type.                 |
| `needs: [build]`                                  | Whether the job exists and the dependency graph contains a cycle.              |
| `uses: ./actions/deploy`                          | The action's metadata, required inputs, declared outputs, and composite steps. |
| A reusable-workflow call                          | Inputs, secrets, outputs, and declared permission constraints.                 |
| `run: ...`                                        | ShellCheck findings in shell scripts and Ruff findings in Python scripts.      |
| `${{ github.event.issue.title }}` inside a script | Whether untrusted text is inserted into executable script text.                |
| A cache write on an untrusted event               | Whether the workflow's event guards and cache permissions allow unsafe access. |
| `runs-on`, trigger filters, or a schedule         | Runner compatibility, glob syntax, cron expressions, and IANA timezones.       |

Findings include source locations, highlighted ranges, explanations, and related locations where available. The [check examples](docs/checks.md) show what each check catches.

## See the mistake before you push it

This workflow defines `matrix.os`, then asks for `matrix.platform`. Save it as `.github/workflows/ci.yml` and run `actionlint check --output-format=pretty .github/workflows/ci.yml`. The Cargo-style report shows the rule name, source location, and failing expression:

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

Change `matrix.platform` to `matrix.os`, check again, and both runner names pass. Other findings can include explanations, related locations, documentation links, and available fixes in the same report.

actionlint also checks expression contexts, action inputs, job outputs, matrix filtering, YAML anchors, and composite-action scripts. It can report errors at the expression that failed, including inside multiline YAML values.

## GitHub Actions features GitHub doesn't even know about

Build an action's inputs with `${{ insert }}`:

```yaml
with:
  ${{ insert }}: >-
    ${{ fromJSON('{"fetch-depth": 0, "persist-credentials": false}') }}
```

actionlint checks the inserted input names and values, even when you build them from matrix expressions. You can insert environment variables into `env:` the same way.

## Use the weird parts of GitHub Actions

Comments inside JSON? Unquoted keys? Trailing text after the closing brace? Mixed-case function names? GitHub accepts them. actionlint understands them.

The [actions-rs CI workflow](https://github.com/kjanat/actions-rs/blob/master/.github/workflows/ci.yml) puts all of that through `fromJSON()`, `toJSON()`, and `fromJSON()` again:

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

actionlint resolves the matrix and reports the loose JSON as information. That workflow also runs YAML-anchored actions in `parallel:` and Rust scripts through `actions-shell cargo-script {0}`, with Cargo dependencies declared inside `run:`.

actionlint still catches its mistakes. `if: ${{ format('{0}', false) }}` produces the string `"false"`, so the job runs. And `${{ github.workspace }}` inside a TOML string can break the Rust script's dependency declaration if the path contains an apostrophe.

<details>
<summary>Build action inputs with JSON comments, unquoted keys, and trailing commas</summary>

The [kp2bw Windows workflow](internal/development/tests/integration/workflow/testdata/repositories/kp2bw-windows-bw-cmd.yml) builds action inputs for npm, Bun, Bitwarden, and Vaultwarden in one expression. Yes, `fromjsOn()` works too:

```yaml
strategy:
  matrix:
    pm: [npm, bun]
    cli: [bw, vw]
    include:
      - ${{ fromJSON('[/* npm ships inside nodejs */ {pm:''bun'', tools:''bun''},] that is all, folks') }}
steps:
  - uses: jdx/mise-action@6612f0ef07ed2a736cd1e00a94a065522dd41195
    with:
      ${{ insert }}: ${{ fromjsOn(format('{{install_args:''{0}'' /* npm ships inside nodejs */,}} that is all, folks', join(fromJSON(format('[''nodejs'',''uv'',''python'',{0},] // bun, if you insist', toJSON(matrix.tools))), ' '))) }}
```

actionlint checks all four combinations. It computes `install_args` for each one, including the extra `bun` for Bun, and checks the resulting action inputs.

</details>

The [shellcheck-rs wiki fixture](internal/development/tests/integration/workflow/testdata/repositories/shellcheck-wiki.yml) also uses `${{ insert }}` in `env:`, expression-valued `with:` mappings, and YAML aliases for action references. The [expression behavior guide](docs/expression-behavior.md) explains the runner comparisons behind these checks, including why `'017' == 17` and `fromJSON('017') == 15` can both be true.

## ShellCheck and Ruff belong in the same report

For a project without configuration, initialize it once:

```sh
actionlint config init --toml
actionlint check
```

Initialization installs the default ShellCheck and Ruff WebAssembly plugins and saves their exact versions in `.github/actionlint.toml`. Commit that file. Local checks, the language server, and CI read the same configuration.

The [Rust port of ShellCheck](https://github.com/kjanat/shellcheck-rs) makes its WebAssembly plugin possible. ShellCheck runs inside actionlint and in the browser playground. You do not need native ShellCheck or Ruff executables for these backends. Their findings point back to the original workflow or action file, with GitHub expressions handled by the host before script analysis.

Use Ruff settings to select Python rules and target versions. Use ShellCheck settings and inline directives to control shell diagnostics. Native ShellCheck and Ruff executables are also supported. See [script-linter configuration](docs/config.md#ruff).

## Preview the fix, then apply it

```sh
actionlint fix
actionlint fix --apply
```

The first command previews available safe edits as JSON. The second applies them and checks the updated files. actionlint rejects conflicting edits and stale source revisions before writing. Add `--include-suggested` when you want to review edits that need a judgment call.

## Get diagnostics and fixes while you edit

Connect your editor to actionlint's language server over standard input and output:

```sh
actionlint lsp
```

The server checks unsaved buffers, updates diagnostics as you edit, and offers versioned quick fixes and suppression actions. Related locations help trace a finding into the local action or reusable workflow that caused it. Expression completion suggests context properties and event fields. See [editor support](docs/lsp.md) for supported features and editor setup.

The [browser playground](https://kjanat.github.io/actionlint) runs the language server in a WebAssembly worker. Paste a workflow, edit it, follow diagnostics to their source, and apply available quick fixes in the browser. It includes ShellCheck for embedded shell scripts and uses the language server for expression completion.

## Check the actions your workflow calls

Remote action metadata gives the checker declared inputs, outputs, and runtimes. Remote reusable-workflow metadata adds input, secret, output, and permission checks across calls.

The bundled Commitish WebAssembly plugin resolves branches, tags and commit IDs. Actionlint fetches the metadata at the selected commit. Cache dependencies before working offline:

```sh
actionlint cache warm .github/workflows
actionlint check --offline .github/workflows
```

Metadata reports distinguish unavailable data from invalid declarations. An optional resolver can also propose immutable commit pins for review. Read [remote dependency checks](docs/remote-actions.md) for resolution, caching, and authentication.

## Configure rules in TOML, with editor completion

Use TOML for configuration. Add rule settings to the generated `.github/actionlint.toml`, keeping its pinned plugin declarations:

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

We recommend [Tombi](https://github.com/tombi-toml/tombi) for your editor's TOML language server. The schema directive connects the file to actionlint's settings for [validation](https://tombi-toml.github.io/tombi/docs/json-schema/) and [completion](https://tombi-toml.github.io/tombi/docs/language-server/completion/). You can complete dotted keys, rule levels, and rule options as you write.

The example sets the security category to `error`, configures two individual rules, and disables one check only for `legacy.yml`. Rule levels are `off`, `on`, `info`, `warn`, and `error`. `--strict` also respects rules you disable.

The rule catalogue covers correctness, suspicious behavior, security, and conventions. It includes checks for empty triggers and steps, string conditions, mixed-type comparisons, missing permissions, unpinned references, runtime lifecycle, and job timeouts. Each rule describes its purpose, options, and examples.

```sh
actionlint rules
actionlint rules matrix
actionlint check --strict
```

YAML, JSON, and JSONC configurations are also supported. Use `extends` to share local or HTTPS configuration files. Declare self-hosted runners, known variables and secrets, and repository permission defaults separately from lint rules.

Inspect the effective settings and their sources:

```sh
actionlint config show --origin
actionlint config validate
actionlint doctor
```

For an existing configuration, preview and apply its migration:

```sh
actionlint config migrate
actionlint config migrate --write
```

Review and commit the changes. See [configuration](docs/config.md), the [rule reference](docs/rules.md), and the [migration guide](docs/config-migration.md).

## Add your own checks

WebAssembly plugins can analyze documents, check embedded languages, or provide runner inventory. A plugin can return diagnostics and source edits with its own rule metadata and configuration schema.

```sh
actionlint plugin search shellcheck
actionlint plugin list
actionlint plugin sync
actionlint plugin update
```

Search finds available plugins. Sync restores the exact configured versions. Update resolves newer releases explicitly. Normal checking restores missing pinned artifacts without upgrading the project.

You can load local artifacts, install registry releases, or pin directly hosted artifacts by SHA-256. actionlint verifies downloaded bytes and applies execution limits. Use `actionlint plugin inspect` to read artifact metadata without executing the plugin.

The [plugin guide](docs/plugins.md) includes SDKs, Rust, C, and TinyGo examples, process-provider manifests, and custom script-linter integrations.

## Send results where you work

The Cargo-style terminal report shows source excerpts, range indicators, related locations, and available fixes. Select another format for CI or your own tools:

```sh
actionlint check --json
actionlint check --output-format=jsonl
actionlint check --output-format=github
actionlint check --output-format=sarif --output-file=actionlint.sarif
```

You can emit multiple formats from one analysis, use custom templates, or read typed JSON with its published schema and TypeScript definitions. JSON includes diagnostic ranges, rule metadata, source references, related locations, and structured workflow and action documents.

The v2 GitHub Action runs the native binary through a Node launcher. It supports annotations, job summaries, verified plugin caching, report files, optional SARIF output, and optional pull-request reviews. It can also install a selected binary version without running checks.

Use [the Action guide](docs/action.md) for setup and available release versions. The [integration guide](docs/usage.md#tools-integration) covers pre-commit, native Git hooks, reviewdog, and editors. See [result formats](docs/results.md) before building a report consumer.

## Get actionlint

The [installation guide](docs/install.md) covers Windows, macOS, Linux, npm, PyPI, Docker, and other package managers.

Try the latest preview from `master`:

```sh
npx https://pkg.pr.new/kjanat/actionlint/@kjanat/actionlint@master
```

Or build from source with Go 1.27 or newer:

```sh
go install ./cmd/actionlint
```

Add Go's installation directory to `PATH` and keep `git` available for repository discovery. Then check a repository, selected files, or a directory:

```sh
actionlint check
actionlint check .github/workflows/ci.yml .github/actions/build/action.yml
actionlint check .github/
```

Discovery respects Git ignore rules and configured file filters. Use `--no-ignore` to include excluded files. Findings produce a nonzero exit status. See [usage](docs/usage.md) for globs, stdin, output options, and shell completion.

The [v2 migration notes](docs/releases/v2.0.0.md) cover plugin dependencies, pre-commit, and result-format changes.

## Build and contribute

Read the [contributing guide](CONTRIBUTING.md), then use the project tools through mise:

```sh
mise exec -- runner install
mise exec -- go build ./cmd/actionlint
mise exec -- run test
mise exec -- run fmt
```

The [repository layout](repository-layout.md) explains where the checker, CLI, plugins, and applications live. Open [an issue](https://github.com/kjanat/actionlint/issues) with a workflow that reproduces a bug or describes a check you need.

## License

actionlint uses the [MIT license](LICENSE.txt). The [identity artwork](docs/assets/brand/LICENSE.md) is by Kaj Kowalski and licensed under CC BY 4.0.

[Support development](https://github.com/sponsors/kjanat).

[docker-badge]: https://img.shields.io/badge/Docker-latest-black?logo=docker&logoColor=white&labelColor=2496ED
[docker-latest]: https://hub.docker.com/r/kjanat/actionlint/tags?name=latest
[docker-nightly-badge]: https://img.shields.io/badge/Docker-nightly-black?logo=docker&logoColor=white&labelColor=2496ED
[docker-nightly]: https://hub.docker.com/r/kjanat/actionlint/tags?name=nightly
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
