# actionlint

[![CI Status][ci-badge]][ci]
[![API Document][apidoc-badge]][apidoc]

[actionlint][repo] is a static checker for GitHub Actions workflow files. [Try it online!][playground]

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

<img src="https://cdn.jsdelivr.net/gh/rhysd/ss@5530c2526b44ad28dc12f91a3d71bcd57940f008/actionlint/main.gif" alt="actionlint reports 7 errors" width="806" height="492"/>

**Example of broken workflow:**

```yaml
on:
  push:
    branch: main
    tags:
      - 'v\d+'
jobs:
  test:
    strategy:
      matrix:
        os: [macos-latest, linux-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - run: echo "Checking commit '${{ github.event.head_commit.message }}'"
      - uses: actions/checkout@v7
      - uses: actions/setup-node@v7
        with:
          node_version: 18.x
      - uses: actions/cache@v6
        with:
          path: ~/.npm
          key: ${{ matrix.platform }}-node-${{ hashFiles('**/package-lock.json') }}
        if: ${{ github.repository.permissions.admin == true }}
      - run: npm install && npm test
```

**actionlint reports 7 errors:**

```console
test.yaml:3:5: unexpected key "branch" for "push" section. expected one of "branches", "branches-ignore", "paths", "paths-ignore", "tags", "tags-ignore", "types", "workflows" [syntax-check]
  |
3 |     branch: main
  |     ^~~~~~~
test.yaml:5:11: character '\' is invalid for branch and tag names. only special characters [, ?, +, *, \, ! can be escaped with \. see `man git-check-ref-format` for more details. note that regular expression is unavailable. note: filter pattern syntax is explained at https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#filter-pattern-cheat-sheet [glob]
  |
5 |       - 'v\d+'
  |           ^~~~
test.yaml:10:28: label "linux-latest" is unknown. available labels are "windows-latest", "windows-latest-8-cores", "windows-2025", "windows-2025-vs2026", windows-2022", "windows-11-arm", "windows-11-vs2026-arm", "ubuntu-slim", "ubuntu-latest", "ubuntu-latest-4-cores", "ubuntu-latest-8-cores", "ubuntu-latest-16-cores", "ubuntu-26.04", "ubuntu-26.04-arm", "ubuntu-24.04", "ubuntu-24.04-arm", "ubuntu-22.04", "ubuntu-22.04-arm", "xcode-27", "xcode-27-xlarge", "macos-latest", "macos-latest-xlarge", "macos-latest-large", "macos-26-intel", "macos-26-xlarge", "macos-26-large", "macos-26", "macos-15-intel", "macos-15-xlarge", "macos-15-large", "macos-15", "macos-14-xlarge", "macos-14-large", "macos-14", "self-hosted", "x64", "arm", "arm64", "linux", "macos", "windows". if it is a custom label for self-hosted runner, set list of labels in actionlint.yaml config file [runner-label]
   |
10 |         os: [macos-latest, linux-latest]
   |                            ^~~~~~~~~~~~~
test.yaml:13:41: "github.event.head_commit.message" is potentially untrusted. avoid using it directly in inline scripts. instead, pass it through an environment variable. see https://docs.github.com/en/actions/reference/security/secure-use#good-practices-for-mitigating-script-injection-attacks for more details [expression]
   |
13 |       - run: echo "Checking commit '${{ github.event.head_commit.message }}'"
   |                                         ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
test.yaml:17:11: input "node_version" is not defined in action "actions/setup-node@v7". available inputs are "architecture", "cache", "cache-dependency-path", "check-latest", "mirror", "mirror-token", "node-version", "node-version-file", "package-manager-cache", "registry-url", "scope", "token" [action]
   |
17 |           node_version: 18.x
   |           ^~~~~~~~~~~~~
test.yaml:21:20: property "platform" is not defined in object type {os: string} [expression]
   |
21 |           key: ${{ matrix.platform }}-node-${{ hashFiles('**/package-lock.json') }}
   |                    ^~~~~~~~~~~~~~~
test.yaml:22:17: receiver of object dereference "permissions" must be type of object but got "string" [expression]
   |
22 |         if: ${{ github.repository.permissions.admin == true }}
   |                 ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
```

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

This repository can be used directly as a Docker action. The prebuilt image includes actionlint, ShellCheck, and pyflakes, and reports problems as GitHub annotations by default. Docker actions require a Linux runner.

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

The moving `v1` tag follows compatible v1 releases. `v1.13.0` is a versioned release tag, but only a full-length commit SHA provides an immutable action reference.

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
  run: echo "$RESULT ($PROBLEM_COUNT problems)"
```

</details>

See [the usage document][usage] for additional examples and output behavior.

## pre-commit

Workflow files can be checked on every commit with [pre-commit][pre-commit]. Add this to `.pre-commit-config.yaml`:

```yaml
---
repos:
  - repo: https://github.com/kjanat/actionlint
    rev: v1.13.0
    hooks:
      - id: actionlint
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

When you see some bugs or false positives, it is helpful to [file a new issue][issue-form] with a minimal example of input. Giving me some feedbacks like feature requests or ideas of additional checks is also welcome.

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
[shellcheck]: https://github.com/koalaman/shellcheck
[pyflakes]: https://github.com/PyCQA/pyflakes
[syntax-doc]: https://docs.github.com/actions/reference/workflow-syntax-for-github-actions
[filter-pattern-doc]: https://docs.github.com/actions/using-workflows/workflow-syntax-for-github-actions#filter-pattern-cheat-sheet
[script-injection-doc]: https://docs.github.com/actions/reference/security/secure-use#good-practices-for-mitigating-script-injection-attacks
[releases]: https://github.com/kjanat/actionlint/releases
[checks]: https://github.com/kjanat/actionlint/blob/v1.13.0/docs/checks.md
[install]: https://github.com/kjanat/actionlint/blob/v1.13.0/docs/install.md
[usage]: https://github.com/kjanat/actionlint/blob/v1.13.0/docs/usage.md
[config]: https://github.com/kjanat/actionlint/blob/v1.13.0/docs/config.md
[api]: https://github.com/kjanat/actionlint/blob/v1.13.0/docs/api.md
[refs]: https://github.com/kjanat/actionlint/blob/v1.13.0/docs/reference.md
[issue-form]: https://github.com/kjanat/actionlint/issues/new
