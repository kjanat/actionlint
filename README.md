# actionlint

[![CI Status][ci-badge]][ci]
[![API Document][apidoc-badge]][apidoc]

[actionlint][repo] is a static checker for GitHub Actions workflow files. [Try it online!][playground]

This is an actively maintained fork of [rhysd/actionlint][upstream]. It carries the upstream checks plus opt-in [policy checks][config], composite action step validation, shell completion, and a first-party GitHub Action, and it ships attested binaries, a Docker image on [GHCR][ghcr] and [Docker Hub][dockerhub], and a Go module at `actionlint.kjanat.dev`. Report problems with this fork [here][issue-form], not upstream.

Features:

- **Syntax check for workflow files** to check unexpected or missing keys following [workflow syntax][syntax-doc]
- **Strong type check for `${{ }}` expressions** to catch several semantic errors like access to not existing property,
  type mismatches, ...
- **Actions usage check** to check that inputs at `with:` and outputs in `steps.{id}.outputs` are correct
- **Reusable workflow check** to check inputs/outputs/secrets of reusable workflows and workflow calls
- **[shellcheck][shellcheck] and [pyflakes][pyflakes] integrations** for scripts at `run:`
- **Security checks**; [script injection][script-injection-doc] by untrusted inputs, hard-coded credentials
- **Other several useful checks**; [glob syntax][filter-pattern-doc] validation, dependencies check for `needs:`, runner label validation, cron syntax validation, ...

See the [full list][checks] of checks done by actionlint.

<picture>
  <source
    media="(prefers-color-scheme: dark)"
    srcset="docs/screenshots/actionlint-dark.gif"
  >
  <source
    media="(prefers-color-scheme: light)"
    srcset="docs/screenshots/actionlint-light.gif"
  >
  <img
    alt="A terminal running actionlint on a workflow file, reporting each problem with the offending line underlined"
    src="docs/screenshots/actionlint-light.gif"
  >
</picture>

<details><summary><h3>Example of a broken workflow</h3></summary>

The same files the animation above records, run through both linters. This section is generated from them by [`scripts/check-readme`](scripts/check-readme), so it cannot drift.

<!-- BEGIN generated demo -->

`docs/screenshots/demo-workflow.yaml`:

```yaml
name: Release
on:
  push:
    branches: [main]
jobs:
  build:
    strategy:
      matrix:
        node: ["20", "22"]
    runs-on: ubuntu-26.04
    timeout-minutes: ${{ matrix.node }}
    steps:
      - uses: actions/checkout@v7
      - run: npm run mock-api
        id: mock
        background: true
      - run: npm test
      - wait: api
```

`docs/screenshots/actionlint.yaml`:

```yaml
policy:
  require-commit-hash: true
```

**Upstream actionlint 1.7.12 reports 3: `runner-label`, `syntax-check` ×2**

```console
demo-workflow.yaml:10:14: label "ubuntu-26.04" is unknown. available labels are "windows-latest", "windows-latest-8-cores", "windows-2025", "windows-2025-vs2026", "windows-2022", "windows-11-arm", "ubuntu-slim", "ubuntu-latest", "ubuntu-latest-4-cores", "ubuntu-latest-8-cores", "ubuntu-latest-16-cores", "ubuntu-24.04", "ubuntu-24.04-arm", "ubuntu-22.04", "ubuntu-22.04-arm", "macos-latest", "macos-latest-xlarge", "macos-latest-large", "macos-26-intel", "macos-26-xlarge", "macos-26-large", "macos-26", "macos-15-intel", "macos-15-xlarge", "macos-15-large", "macos-15", "macos-14-xlarge", "macos-14-large", "macos-14", "self-hosted", "x64", "arm", "arm64", "linux", "macos", "windows". if it is a custom label for self-hosted runner, set list of labels in actionlint.yaml config file [runner-label]
   |
10 |     runs-on: ubuntu-26.04
   |              ^~~~~~~~~~~~
demo-workflow.yaml:16:9: unexpected key "background" for step to run shell command. expected one of "continue-on-error", "env", "id", "if", "name", "run", "shell", "timeout-minutes", "working-directory" [syntax-check]
   |
16 |         background: true
   |         ^~~~~~~~~~~
demo-workflow.yaml:18:9: step must run script with "run" section or run action with "uses" section [syntax-check]
   |
18 |       - wait: api
   |         ^~~~~
```

**This fork 1.14.0 reports 3: `expression`, `require-commit-hash`, `parallel-steps`**

```console
demo-workflow.yaml:11:22: type of expression at "float number value" must be number but found type string [expression]
   |
11 |     timeout-minutes: ${{ matrix.node }}
   |                      ^~~
demo-workflow.yaml:13:15: the ref "v7" of action "actions/checkout@v7" is not a commit SHA. actions must be pinned to a full-length commit SHA (40 or 64 hexadecimal digits) because "require-commit-hash" is enabled in the "policy" configuration. see https://docs.github.com/en/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions#using-third-party-actions for more details [require-commit-hash]
   |
13 |       - uses: actions/checkout@v7
   |               ^~~~~~~~~~~~~~~~~~~
demo-workflow.yaml:18:15: "api" is not the ID of a preceding background step. "wait" and "cancel" steps can only refer to an earlier step that has "background: true" [parallel-steps]
   |
18 |       - wait: api
   |               ^~~
```

<!-- END generated demo -->

</details>

## Quick start

Install `actionlint` command by downloading [the released binary][releases], using the download script, running the Docker image, using the repository as a GitHub Action, or by `go install`. See
[the installation document][install] for more details like how to manage the command with several package managers or run via Docker container.

```sh
go install actionlint.kjanat.dev/cmd/actionlint@latest
```

Basically all you need to do is run the `actionlint` command in your repository. actionlint automatically detects workflows and checks errors. actionlint focuses on finding out mistakes. It tries to catch errors as much as possible and make false positives as minimal as possible.

```sh
actionlint
```

Another option to try actionlint is [the online playground][playground]. Your browser can run actionlint through WebAssembly.

See [the usage document][usage] for more details.

## GitHub Action

This repository can be used directly as a Docker action. The prebuilt image includes actionlint, ShellCheck, and pyflakes, and reports problems as GitHub annotations by default. Docker container actions run only on Linux, and this action also needs a reachable Docker daemon. `ubuntu-slim` is not supported: its job runs in an unprivileged container with the Docker client but no daemon or Docker socket. Standard Ubuntu runners, including `ubuntu-24.04-arm` and `ubuntu-26.04-arm`, are supported by the published `linux/amd64` and `linux/arm64` image.

```yaml
name: Lint GitHub Actions workflows
on: [push, pull_request]

jobs:
  actionlint:
    runs-on: ubuntu-latest
    steps:
      - { uses: actions/checkout@v7, with: { persist-credentials: false } }
      - uses: kjanat/actionlint@v1
```

On a daemon-less runner such as `ubuntu-slim`, download and run the binary instead:

```yaml
- uses: actions/checkout@v7
  with: { persist-credentials: false }
- name: Download and run actionlint
  env: { GH_TOKEN: "${{ github.token }}", GH_REPO: "kjanat/actionlint" }
  run: |
    case "${RUNNER_ARCH}" in
      X64) asset_arch=amd64 ;;
      ARM64) asset_arch=arm64 ;;
      ARM) asset_arch=armv6 ;;
      X86) asset_arch=386 ;;
      *) echo "Unsupported runner architecture: ${RUNNER_ARCH}" >&2; exit 1 ;;
    esac
    gh release download --pattern "actionlint_*_${RUNNER_OS,,}_${asset_arch}.tar.gz" --output - | tar -xzf - actionlint
    ./actionlint -color
```

The moving `v1` tag follows compatible v1 releases. `v1.14.0` is a versioned release tag, but only a full-length commit SHA provides an immutable action reference.

<details><summary><h3>Inputs</h3></summary>

| Input               | Default       | Description                                                                                  |
| ------------------- | ------------- | -------------------------------------------------------------------------------------------- |
| `files`             | all workflows | Newline-separated workflow paths. Empty checks every workflow in the repository.             |
| `format`            | `github`      | Output format: `github`, `default`, `oneline`, `json`, `json-lines`, `markdown`, or `sarif`. |
| `ignore`            | none          | Newline-separated regular expressions for actionlint errors to ignore.                       |
| `config-file`       | automatic     | Configuration file path relative to `working-directory`.                                     |
| `shellcheck`        | `true`        | Run ShellCheck for shell scripts in workflow steps.                                          |
| `pyflakes`          | `true`        | Run pyflakes for Python scripts in workflow steps.                                           |
| `working-directory` | `.`           | Directory to lint, relative to the repository workspace.                                     |
| `output-file`       | none          | Repository-relative file to receive the selected output format.                              |
| `fail-on-error`     | `true`        | Fail when problems are found. Invalid options and fatal errors always fail.                  |

</details>
<details><summary><h3>Outputs</h3></summary>

| Output           | Description                                                                                         |
| ---------------- | --------------------------------------------------------------------------------------------------- |
| `exit-code`      | actionlint exit code: `0` for clean, `1` for problems, `2` for invalid options, or `3` for failure. |
| `result`         | `success`, `problems-found`, `invalid-options`, or `failure`.                                       |
| `problems-found` | Whether actionlint found one or more problems.                                                      |
| `problem-count`  | Number of problems, or an empty string if actionlint could not complete.                            |
| `output`         | Complete actionlint output in the selected format.                                                  |
| `output-file`    | Repository-relative output path, or an empty string when no file was requested.                     |

Give the step an `id` to consume its outputs. For example, this writes JSON Lines without failing the lint step:

```yaml
- name: Check workflows
  id: actionlint
  uses: kjanat/actionlint@v1
  with:
    format: json-lines
    output-file: actionlint-results.jsonl
    fail-on-error: false
- name: Report result
  if: always()
  env:
    RESULT: ${{ steps.actionlint.outputs.result }}
    PROBLEM_COUNT: ${{ steps.actionlint.outputs.problem-count }}
  run: echo "${RESULT} (${PROBLEM_COUNT} problems)"
```

</details>

See [the usage document][usage] for additional examples and output behavior.

## pre-commit

Workflow files can be checked on every commit with [pre-commit][pre-commit]. Add this to `.pre-commit-config.yaml`:

```yaml
---
repos:
  - repo: https://github.com/kjanat/actionlint
    rev: v1.14.0
    hooks: [id: actionlint]
```

<details><summary><h3>Choosing a hook</h3></summary>

Four hooks check `.github/workflows/` the same way and differ only in where the `actionlint` executable comes from.

| Hook ID                 | Where the executable comes from                           | Requires                    |
| ----------------------- | --------------------------------------------------------- | --------------------------- |
| `actionlint`            | Built from this repository into an isolated `$GOPATH`.    | Go toolchain                |
| `actionlint-shellcheck` | Same, plus a Go build of ShellCheck installed next to it. | Go toolchain                |
| `actionlint-docker`     | Pulls this repository's image from `ghcr.io`.             | Docker                      |
| `actionlint-system`     | Runs the `actionlint` already on `PATH`.                  | [A manual install][install] |

The `actionlint` hook installs into an isolated `$GOPATH`, so [the ShellCheck integration][checks] finds a `shellcheck` executable only when one is already on `PATH`. `actionlint-shellcheck` supplies one itself, which is the option to pick when contributors should not have to install ShellCheck.

</details>

See [the usage document][usage] for the pinned ShellCheck build and how to choose a different one.

## Documents

- [Checks][checks]: Full list of all checks done by actionlint with example inputs, outputs, and playground links.
- [Installation][install]: Installation instructions. Prebuilt binaries, a Docker image, building from source, a download script (for CI), supports by several package managers are available.
- [Usage][usage]: How to use `actionlint` command locally or on GitHub Actions, the online playground, an official Docker image, and integrations with reviewdog, Problem Matchers, super-linter, pre-commit, VS Code.
- [Configuration][config]: How to configure actionlint behavior. Currently, the labels of self-hosted runners, the configuration variables, and ignore patterns of errors for each file paths can be set.
- [Go API][api]: How to use actionlint as Go library.
- [References][refs]: Links to resources.

## Bug reporting

When you see some bugs or false positives, it is helpful to [file a new issue][issue-form] with a minimal example of input. Feature requests and ideas for additional checks are welcome too.

See the [contribution guide](./CONTRIBUTING.md) for more details.

## License

actionlint is distributed under [the MIT license](./LICENSE.txt).

[ci-badge]: https://github.com/kjanat/actionlint/actions/workflows/ci.yaml/badge.svg
[ci]: https://github.com/kjanat/actionlint/actions/workflows/ci.yaml
[apidoc-badge]: https://pkg.go.dev/badge/actionlint.kjanat.dev.svg
[apidoc]: https://pkg.go.dev/actionlint.kjanat.dev
[repo]: https://github.com/kjanat/actionlint
[playground]: https://kjanat.github.io/actionlint/
[pre-commit]: https://pre-commit.com
[upstream]: https://github.com/rhysd/actionlint
[ghcr]: https://github.com/kjanat/actionlint/pkgs/container/actionlint
[dockerhub]: https://hub.docker.com/r/kjanat/actionlint
[shellcheck]: https://github.com/koalaman/shellcheck
[pyflakes]: https://github.com/PyCQA/pyflakes
[syntax-doc]: https://docs.github.com/actions/reference/workflow-syntax-for-github-actions
[filter-pattern-doc]: https://docs.github.com/actions/using-workflows/workflow-syntax-for-github-actions#filter-pattern-cheat-sheet
[script-injection-doc]: https://docs.github.com/actions/reference/security/secure-use#good-practices-for-mitigating-script-injection-attacks
[releases]: https://github.com/kjanat/actionlint/releases
[checks]: https://github.com/kjanat/actionlint/blob/v1.14.0/docs/checks.md
[install]: https://github.com/kjanat/actionlint/blob/v1.14.0/docs/install.md
[usage]: https://github.com/kjanat/actionlint/blob/v1.14.0/docs/usage.md
[config]: https://github.com/kjanat/actionlint/blob/v1.14.0/docs/config.md
[api]: https://github.com/kjanat/actionlint/blob/v1.14.0/docs/api.md
[refs]: https://github.com/kjanat/actionlint/blob/v1.14.0/docs/reference.md
[issue-form]: https://github.com/kjanat/actionlint/issues/new
