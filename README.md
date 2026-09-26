# actionlint

[![CI Status][ci-badge]][ci]
[![API Document][apidoc-badge]][apidoc]
[![Sponsor this project][sponsor-badge]][sponsor]

[actionlint][repo] is a static checker for GitHub Actions workflow files. [Try it online!][playground]

This is an actively maintained fork of [rhysd/actionlint][upstream]. It carries the upstream checks plus cache safety policies enabled by default, configurable opt-in [policy checks][config], composite action step validation, shell completion, and a first-party GitHub Action, and it ships attested binaries, a Docker image on [GHCR][ghcr] and [Docker Hub][dockerhub], and a Go module at `actionlint.kjanat.dev`. Report problems through [this fork's issue tracker][issue-form].

## Fork provenance and versioning

This fork builds on upstream [v1.7.12](https://github.com/rhysd/actionlint/releases/tag/v1.7.12) ([`rhysd/actionlint@914e7df`]). Its independent release series starts at v1.8.0. Tags published by `kjanat/actionlint` identify this fork's releases; a matching tag need not exist in `rhysd/actionlint`.

The [changelog] separates fork releases from inherited upstream history. Releases follow [Semantic Versioning for the supported CLI and configuration contracts], with [CLI compatibility] maintained for existing integrations. New or corrected lint findings can affect CI results; the [Go library API remains unstable].

## Features

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

The same files the animation above records, run through both linters. This section is generated from them by [`scripts/check-readme`].

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
# yaml-language-server: $schema=https://cdn.jsdelivr.net/npm/@kjanat/actionlint/actionlint.schema.json
---
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

**This fork 1.17.0 reports 3: `expression`, `require-commit-hash`, `parallel-steps`**

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

Install with [npm], [Homebrew], [AUR],
[Scoop], or [mise], or download [a release archive][releases].
See [the installation document][install] for all options. To run it through npm:

```sh
npx @kjanat/actionlint
```

With a Go toolchain, install it from source:

```sh
go install actionlint.kjanat.dev/cmd/actionlint@latest
```

<sub><em><code>actionlint.kjanat.dev</code> is a <a href="https://pkg.go.dev/cmd/go#hdr-Fully_qualified_import_paths" title="Fully-qualified import paths">Go vanity import path</a>. The Go toolchain resolves it to <a href="https://github.com/kjanat/actionlint" title="https://github.com/kjanat/actionlint">this GitHub repository</a>, where the source, releases, and issue tracker live. Further reading available here: <a href="https://go.dev/ref/mod#goproxy-protocol" title="GOPROXY protocol">GOPROXY protocol</a>. Check for yourself: </em><code>curl https://proxy.golang.org/actionlint.kjanat.dev/@latest</code></sub>

Basically all you need to do is run the `actionlint` command in your repository. actionlint automatically detects workflows and checks errors. actionlint focuses on finding out mistakes. It tries to catch errors as much as possible and make false positives as minimal as possible.

```sh
actionlint
```

Another option to try actionlint is [the online playground][playground]. Your browser can run actionlint through WebAssembly.

See [the usage document][usage] for more details.

## GitHub Action

Lint with annotations and a compact job summary:

```yaml
name: Lint workflows
on: [push, pull_request]
permissions: { contents: read }
jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with: { persist-credentials: false }
      - uses: kjanat/actionlint@v1
```

Project settings belong in [`.github/actionlint.yaml`](docs/config.md).
Use the `config` input for a one-run override. No `with:` block is required.

The upcoming JavaScript Action runs on Linux, macOS and Windows without Docker.
Existing immutable releases retain their Docker implementation. Pin a published
release commit SHA or normal `vX.Y.Z` tag; `v1` and `v1.X` are moving pointers.

See the [Action guide](docs/action.md) for configuration, saved reports,
optional PR reviews, tool prerequisites, migration and the input/output reference.

## pre-commit

Workflow files can be checked on every commit with [pre-commit][pre-commit]. Add this to `.pre-commit-config.yaml`:

```yaml
---
repos:
  - repo: https://github.com/kjanat/actionlint
    rev: v1.17.0
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

- [AI usage and policy] and [CLI compatibility]: How this fork uses AI and approaches compatibility for existing workflows and CI integrations.
- [Checks][checks]: Full list of all checks done by actionlint with example inputs, outputs, and playground links.
- [Installation][install]: Install with npm, Homebrew, AUR, Scoop, aqua, mise, the community pip/uv wrapper, release archives, the download script, Docker, or Go. Includes the status of WinGet and upstream-only package names.
- [Usage][usage]: How to use `actionlint` command locally or on GitHub Actions, the online playground, an official Docker image, and integrations with reviewdog, Problem Matchers, super-linter, pre-commit, VS Code.
- [Configuration][config]: Runner labels, variables, secrets, default permissions, error filters, and opt-in policy checks, with YAML Language Server schema support.
- [Go API][api]: How to use actionlint as Go library.
- [Schema audit]: Pinned upstream definitions, compatibility fixes, validation evidence, and retained differences.
- [Expression behavior]: Reproduce GitHub's runtime behavior, understand parser differences, and avoid surprising workflow decisions.
- [References][refs]: Links to resources.
- [GitHub Actions changelog][github-changelog]: Browse and search the latest entries from GitHub's Actions changelog feed.

## Bug reporting

When you see some bugs or false positives, it is helpful to [file a new issue][issue-form] with a minimal example of input. Feature requests and ideas for additional checks are welcome too.

See the [contribution guide] for more details.

## License

actionlint is distributed under [the MIT license].

[AI usage and policy]: CONTRIBUTING.md#ai-usage-and-policy
[AUR]: docs/install.md#arch-linux
[CLI compatibility]: CONTRIBUTING.md#cli-compatibility
[Expression behavior]: docs/expression-behavior.md
[Go library API remains unstable]: CONTRIBUTING.md#go-library-api
[Homebrew]: docs/install.md#homebrew
[Schema audit]: docs/schema-audit.md
[Scoop]: docs/install.md#scoop
[Semantic Versioning for the supported CLI and configuration contracts]: CONTRIBUTING.md#release-versioning
[`scripts/check-readme`]: scripts/check-readme
[apidoc-badge]: https://pkg.go.dev/badge/actionlint.kjanat.dev.svg
[apidoc]: https://pkg.go.dev/actionlint.kjanat.dev
[changelog]: CHANGELOG.md#upstream-history
[ci-badge]: https://github.com/kjanat/actionlint/actions/workflows/ci.yml/badge.svg
[ci]: https://github.com/kjanat/actionlint/actions/workflows/ci.yml
[config]: docs/config.md
[contribution guide]: ./CONTRIBUTING.md
[dockerhub]: https://hub.docker.com/r/kjanat/actionlint
[filter-pattern-doc]: https://docs.github.com/actions/using-workflows/workflow-syntax-for-github-actions#filter-pattern-cheat-sheet
[ghcr]: https://github.com/kjanat/actionlint/pkgs/container/actionlint
[github-changelog]: https://actionlint.kjanat.dev/github-changelog/
[issue-form]: https://github.com/kjanat/actionlint/issues/new
[mise]: docs/install.md#mise
[npm]: docs/install.md#npm
[playground]: https://kjanat.github.io/actionlint/
[pre-commit]: https://pre-commit.com
[pyflakes]: https://github.com/PyCQA/pyflakes
[releases]: https://github.com/kjanat/actionlint/releases
[repo]: https://github.com/kjanat/actionlint
[script-injection-doc]: https://docs.github.com/actions/reference/security/secure-use#good-practices-for-mitigating-script-injection-attacks
[shellcheck]: https://github.com/koalaman/shellcheck
[sponsor-badge]: https://img.shields.io/badge/Sponsor-ea4aaa?logo=githubsponsors&logoColor=white
[sponsor]: https://github.com/sponsors/kjanat
[syntax-doc]: https://docs.github.com/actions/reference/workflow-syntax-for-github-actions
[the MIT license]: ./LICENSE.txt
[upstream]: https://github.com/rhysd/actionlint

<!-- versioned links -->

[api]: https://github.com/kjanat/actionlint/blob/v1.17.0/docs/api.md
[checks]: https://github.com/kjanat/actionlint/blob/v1.17.0/docs/checks.md
[install]: https://github.com/kjanat/actionlint/blob/master/docs/install.md
[refs]: https://github.com/kjanat/actionlint/blob/v1.17.0/docs/reference.md
[usage]: https://github.com/kjanat/actionlint/blob/v1.17.0/docs/usage.md

<!-- specific commit refs -->

[`rhysd/actionlint@914e7df`]: https://github.com/rhysd/actionlint/commit/914e7df21a07ef503a81201c76d2b11c789d3fca
