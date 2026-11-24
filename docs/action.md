# GitHub Action

v2 is not published yet.

## Quick start

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
      - uses: kjanat/actionlint@v2
```

Pin a release commit SHA or a `vX.Y.Z` tag. Major and minor tags move.

## Permissions

Analysis, reporting, installation, `plugin-cache` and artifact uploads need no `GITHUB_TOKEN` permissions. Two features need grants:

| Feature                      | Required access                                                                                                                |
| ---------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| `review: true`               | `pull-requests: write` on the PR's repository and read access to the PR head's content. See [PR reviews](#optional-pr-review). |
| SARIF upload in a later step | `security-events: write`, plus `actions: read` in private repositories. See [SARIF upload](#upload-sarif-to-code-scanning).    |

With reviews off and no authenticated checkout, `permissions: {}` works.

Only PR reviews use the `token` input. It defaults to `${{ github.token }}`. A custom GitHub App or fine-grained token needs [Pull requests: write](https://docs.github.com/en/rest/pulls/reviews#create-a-review-for-a-pull-request) on the PR's repository and [Contents: read](https://docs.github.com/en/rest/repos/contents#get-repository-content) for private PR-head content.

## Install without running analysis

```yaml
- uses: kjanat/actionlint@v2
  id: setup
  with:
    install-only: true
    version: 1.17.0
- run: actionlint --version
- run: actionlint .github/workflows/ci.yml
```

`install-only` exports the binary for later steps and reads no workflows or configuration. Later steps get no plugins and no native ShellCheck or Ruff. Provision those separately.

Use `version-file: .tool-versions` to read a checked-in pin:

```text
nodejs 24.21.0
actionlint 1.17.0
```

A file that contains only `1.17.0` also works. The path resolves from `working-directory`. The entry must name one exact release. Ranges, `system` and path selectors are unsupported. `version` overrides `version-file`. With neither input, the binary matches the Action release.

Install-only works with older releases. Linting needs a v2 binary. Use the v1 Action to lint with an older release.

In this mode `result` is `success`, `exit-code` is `0`, and the report outputs are empty.

## Configuration

```yaml
lint:
  rules:
    correctness:
      runner-label:
        level: default
        options: { labels: [ubuntu-24.04-custom] }
tools:
  shellcheck:
    config:
      enable: [quote-safe-variables]
```

A `config-file` that does not exist fails the step.

Use `config` for a per-step overlay:

```yaml
- uses: kjanat/actionlint@v2
  with:
    config: |
      lint:
        rules:
          correctness:
            expression-reference:
              level: default
              options: { variables: [DEPLOY_ENV] }
      tools:
        shellcheck:
          config:
            disable: [SC2086]
```

Or pass a repository variable that holds YAML or JSON:

```yaml
- uses: kjanat/actionlint@v2
  with:
    config: ${{ vars.ACTIONLINT_CONFIG }}
```

The overlay merges over the selected file. Maps merge recursively. Lists and scalars replace. `tools: {shellcheck: {config: null}}` clears ShellCheck's configuration.

### Path bases

| Value                    | Relative to                                               |
| ------------------------ | --------------------------------------------------------- |
| `working-directory`      | `GITHUB_WORKSPACE`. Absolute paths also work.             |
| `files`, `config-file`   | Action `working-directory`.                               |
| Rc path in a config file | The directory that contains that config file.             |
| `output-file`            | `GITHUB_WORKSPACE`, even when `working-directory` is set. |

## Reports

| Destination               | Control                                                                 |
| ------------------------- | ----------------------------------------------------------------------- |
| Console / legacy `output` | `format`, default `github`.                                             |
| Finding annotations       | `annotations: auto` follows `format`; `true` enables, `false` disables. |
| Job summary               | `summary: true` by default.                                             |
| Versioned JSON file       | Always written; `result-file` output.                                   |
| SARIF file                | `sarif: true`; `report-sarif` output.                                   |

Set `summary-title` to label each summary when a job runs the Action more than once.

Save reports even when findings fail the step:

```yaml
- uses: kjanat/actionlint@v2
  id: lint
  with: { sarif: true }
- uses: actions/upload-artifact@v7
  if: always() && steps.lint.outputs.result-file != ''
  with:
    name: actionlint-results
    path: |
      ${{ steps.lint.outputs.result-file }}
      ${{ steps.lint.outputs.report-sarif }}
```

See [the result contract](results.md).

Report files last until the job ends. Upload them to use them elsewhere. A missing report never means a clean run.

### Upload SARIF to code scanning

`sarif: true` writes a SARIF file and exposes its path as `report-sarif`. To publish it to code scanning, add an uploader with its [required permissions](https://docs.github.com/en/code-security/how-tos/find-and-fix-code-vulnerabilities/integrate-with-existing-tools/upload-sarif-file):

```yaml
name: Scan workflows
on: [push, pull_request]
permissions:
  contents: read
  security-events: write
  actions: read # Required by the uploader for private repositories.
jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with: { persist-credentials: false }
      - uses: kjanat/actionlint@v2
        id: lint
        with: { sarif: true }
      - uses: github/codeql-action/upload-sarif@v4
        if: always() && steps.lint.outputs.report-sarif != ''
        with:
          sarif_file: ${{ steps.lint.outputs.report-sarif }}
```

The repository must have code scanning enabled.

### Optional PR review

```yaml
name: Review workflows
on: pull_request
permissions: { contents: read, pull-requests: write }
jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with: { persist-credentials: false }
      - uses: kjanat/actionlint@v2
        with: { review: true }
```

Reviews comment on changed lines, add suggestions where they apply, and skip comments that already exist. Fork and Dependabot PRs get a read-only `GITHUB_TOKEN` whatever the workflow requests. A failed review leaves the analysis result intact. Keep untrusted PRs on `pull_request`. Never run PR code under `pull_request_target` to get a write token. See [GitHub's fork permission rules](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#changing-the-permissions-in-a-forked-repository).

## Plugins and binary installation

The Action honors HTTP(S) proxy settings. The runner must support Node 24. The Action runs on Linux, macOS and Windows on x64 and arm64.

The Action neither runs nor downloads a disabled tool.

## Input/output reference

| Input                            | Default        | Meaning                                                                    |
| -------------------------------- | -------------- | -------------------------------------------------------------------------- |
| `version`                        | Action release | Exact binary release. Overrides `version-file`.                            |
| `version-file`                   | empty          | Exact version file or `.tool-versions`, relative to `working-directory`.   |
| `install-only`                   | `false`        | Install actionlint on PATH without analysis or reporting.                  |
| `plugin-cache`                   | `true`         | Restore and save digest-verified plugin artifacts.                         |
| `files`                          | discover       | Literal newline-separated workflow paths.                                  |
| `format`                         | `github`       | `github`, `default`, `oneline`, `json`, `json-lines`, `markdown`, `sarif`. |
| `ignore`                         | empty          | Newline-separated regular expressions.                                     |
| `config-file`                    | discover       | Explicit config file.                                                      |
| `config`                         | inherit        | Inline YAML/JSON overlay.                                                  |
| `working-directory`              | `.`            | Analysis directory.                                                        |
| `disable-tools`                  | empty          | Tool ids to turn off, separated by newlines or commas.                     |
| `shellcheck`, `ruff`, `pyflakes` | none           | Ignored. Use `disable-tools`.                                              |
| `add-actionlint-to-path`         | `true`         | Export actionlint for later steps.                                         |
| `output-file`                    | empty          | Workspace-relative legacy-format report.                                   |
| `fail-on-error`                  | `true`         | Fail the step on findings. Invalid options and failures always fail.       |
| `annotations`                    | `auto`         | Finding annotation control.                                                |
| `summary`                        | `true`         | Compact job summary.                                                       |
| `summary-title`                  | empty          | Summary and log label, such as `Production workflows`.                     |
| `sarif`                          | `false`        | Write an additional SARIF file.                                            |
| `review`                         | `false`        | Post an advisory PR review. See [permissions](#permissions).               |
| `token`                          | `github.token` | Token for PR reviews. See [permissions](#permissions).                     |

| Output           | Meaning                                                              |
| ---------------- | -------------------------------------------------------------------- |
| `version`        | Installed binary version, without the `v` prefix.                    |
| `result`         | `success`, `problems-found`, `invalid-options`, `failure`.           |
| `problems-found` | Whether completed analysis found problems.                           |
| `problem-count`  | Count, or empty when analysis did not complete.                      |
| `output`         | Selected serialization. Prefer files for large reports.              |
| `output-file`    | Workspace-relative requested path, or empty.                         |
| `result-file`    | Absolute path of the versioned JSON result, also written on failure. |
| `report-sarif`   | Absolute SARIF path, or empty.                                       |

## Migration and troubleshooting

JSON output is the versioned result object. Update consumers with the [migration guide](results.md) before upgrading.

For a standalone binary or CLI Docker image, see [manual installation](usage.md#manual-installation).
