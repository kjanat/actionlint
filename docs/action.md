# GitHub Action

The upcoming Node 24 Action runs the ordinary actionlint binary without Docker.
Existing immutable releases keep their original implementation. Examples using
new inputs require a release containing this change.

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
      - uses: kjanat/actionlint@v1
```

This discovers workflows, reports findings as annotations, and writes a compact
job summary. Findings fail the step. Pin a **published release commit SHA** or
normal `vX.Y.Z` tag for immutable consumption; major/minor tags move.

## Configuration

Keep reusable settings in `.github/actionlint.yaml`, shared with the CLI:

```yaml
self-hosted-runner:
  labels: [ubuntu-24.04-custom]
tools:
  shellcheck:
    config:
      enable: [check-unassigned-uppercase]
```

The quick-start workflow needs no changes. `.yaml` wins over `.yml` if both
exist. No config means defaults; an explicit missing `config-file` is an error.
The log identifies the selected file and any inline override.

Use `config` for an invocation-specific overlay:

```yaml
- uses: kjanat/actionlint@v1
  with:
    config: |
      config-variables: [DEPLOY_ENV]
      tools:
        shellcheck:
          config:
            disable: [SC2086]
```

Or pass a repository variable containing YAML/JSON directly:

```yaml
- uses: kjanat/actionlint@v1
  with:
    config: ${{ vars.ACTIONLINT_CONFIG }}
```

`toJSON(...)` is needed only when the source expression is an object, not already
a YAML/JSON string. The real config file offers editor completion, hover and
validation; editors generally treat `with.config` as a string.

### Precedence and resets

The overlay merges over the selected file. Maps merge recursively; lists and
scalars replace. These examples start with `config-variables: [DEPLOY_ENV]` and
`tools.shellcheck.config: {disable: [SC2086], enable: [all]}`:

| Overlay                                                  | Effect                                                          |
| -------------------------------------------------------- | --------------------------------------------------------------- |
| Omitted or blank input                                   | Inherit the file.                                               |
| `config: '{}'`                                           | Keep inherited settings.                                        |
| `config: 'null'`                                         | Reset the complete config to defaults.                          |
| `config: 'config-variables: []'`                         | Allow no variables.                                             |
| `config: 'config-variables: null'`                       | Restore the default: no variable-name checking.                 |
| `config: 'tools: {shellcheck: false}'`                   | Disable ShellCheck, retaining its settings for a later overlay. |
| `config: 'tools: {shellcheck: {config: {disable: []}}}'` | Clear disabled codes; retain `enable: [all]`.                   |
| `config: 'tools: {shellcheck: {config: .shellcheckrc}}'` | Replace the inline directive map with an rc path.               |
| `config: 'tools: {shellcheck: {config: null}}'`          | Clear rc/inline configuration.                                  |

An empty map inherits **a map**. Switching between an rc string and an inline
mapping replaces the old form; `{}` after an rc string selects an empty mapping.
Unknown inline keys fail with their location. Historically ignored file keys warn.

`shellcheck: false` and `pyflakes: false` disable those integrations. The default
`shellcheck: true` permits the tool; it does not override `tools.shellcheck: false`.
For ShellCheck flags, explicit `shellcheck-args` overrides `SHELLCHECK_OPTS`.
Scalar options use the last value; list options accumulate without duplicates.
`shellcheck-rc` overrides rc-selection flags. Inline directives remain active when
rc discovery is disabled. See [dialect precedence and directives](config.md#script-selection-and-directives).

### Path bases

| Value                                               | Relative to                                                           |
| --------------------------------------------------- | --------------------------------------------------------------------- |
| `working-directory`                                 | `GITHUB_WORKSPACE`; absolute paths also work.                         |
| `files`, `config-file`, `shellcheck-rc`, `--rcfile` | Action `working-directory`.                                           |
| Rc path in the config file                          | Directory containing that config file.                                |
| Rc path supplied through `config`                   | Action `working-directory`; inherited paths keep their original base. |
| `${{ configdir }}` in config                        | Selected config file's directory.                                     |
| Inline `source-path`                                | Analyzed step's effective working directory.                          |
| `output-file`                                       | `GITHUB_WORKSPACE`, for compatibility.                                |

For a checkout at `app`, use `working-directory: app` and
`config-file: .github/actionlint.yaml`; `output-file: reports/lint.json` still
writes under the workspace's `reports/`, outside `app`.

Paths are literal; `files` is newline-separated and does not expand globs.
`ignore` is newline-separated regex text: quotes inside `ignore: |` are literal.
Config interpolation supports a [small set of path contexts](config.md#shellcheck),
not arbitrary GitHub expressions. GitHub evaluates expressions in workflow inputs
first; a checked-in config file avoids escaping nested expressions.

## Reports

| Destination               | Control                                                                 |
| ------------------------- | ----------------------------------------------------------------------- |
| Console / legacy `output` | `format`, default `github`.                                             |
| Finding annotations       | `annotations: auto` follows `format`; `true` enables, `false` disables. |
| Job summary               | `summary: true` by default.                                             |
| Versioned JSON file       | Always written; `result-file` output.                                   |
| SARIF file                | `sarif: true`; `report-sarif` output.                                   |
| PR comments               | `review: true`, opt-in.                                                 |

Explicit annotations off preserves the serialized `output` value. Fatal errors
still report a step error. Multiple destinations share one analysis.

Save reports even when findings fail the lint step:

```yaml
- uses: kjanat/actionlint@v1
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

`result-file` contains a [versioned result](../packages/github-action/result.schema.json):

```json
{
  "schema_version": 1,
  "status": "success",
  "completed": true,
  "exit_code": 0,
  "file_count": 1,
  "diagnostics": [],
  "configurations": [],
  "hints": []
}
```

This differs from legacy Action `format: json` (a diagnostic array) and CLI
`check --output-format=json` (a diagnostic envelope). Neither old format changes.
The new result includes completion, counts, config origins, hints, and available
source/fix information. Full workflow/job/step inventories are separate work
(kjanat/actionlint#189).

Files use unique absolute paths and survive for later steps in the **same job**.
Upload them for use elsewhere. A result is attempted for setup/input failures too;
failure to create/write files can prevent that output. Missing output is never
evidence of a clean run. Incomplete results retain known diagnostics; `file_count`
may be null. SARIF is exposed only for completed analysis. `fail-on-error: false`
changes the step status for findings, not the recorded analysis outcome.

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
      - uses: kjanat/actionlint@v1
        with: { review: true }
```

Reviews group findings on changed lines and add applicable suggestions. Reruns
deduplicate existing comments. Missing PR context, unchanged lines, mismatched
source, limits and permission errors produce concise feedback; analysis files
remain available. Fork PR tokens may lack write permission. Keep the ordinary
`pull_request` event rather than running untrusted checkout code with a privileged
`pull_request_target` token. Review failure is advisory, not a clean-analysis claim.

## Tools and advanced ShellCheck

Each invocation downloads the **version-matched ordinary binary**, verifies its
checksum and executes it. It does not reuse a cached actionlint binary. ShellCheck
and Pyflakes use a suitable PATH installation, then the tool cache, then a pinned
download. Pyflakes needs Python 3.9 or newer; the Action does not install Python.
Tool source/version details appear in debug logs; enable runner step debugging.

Each `add-*-to-path` switch independently exports that selected tool to subsequent
steps, default on. Disabled integrations export nothing. Exports last for this job.
Node 24 support is required from the runner. Linux, macOS and Windows x64/arm64
are handled by the launcher; actual availability also depends on the corresponding
release assets and optional tools. CI tests its configured hosted runner matrix.
Native downloads require network access, including on reruns; existing optional
tools do not make the Action offline. HTTP(S) proxy settings are honored by downloads.

Common directives belong in `tools.shellcheck.config`. Advanced invocation options:

```yaml
- uses: kjanat/actionlint@v1
  with:
    shellcheck-rc: true
    shellcheck-args: '["--severity=warning", "--enable=all"]'
```

Blank `shellcheck-rc` inherits rc selection. `true` searches upward from the Action
working directory to the filesystem root, then checks user configuration:
`~/.shellcheckrc` and `$XDG_CONFIG_HOME/shellcheckrc` (or `~/.config/shellcheckrc`)
on Unix, or the user config directory's `shellcheckrc` on Windows. `false` disables rc loading;
a path selects a readable file. Project config also accepts an rc directory.
Arguments are a literal YAML/JSON string array, without shell expansion.
Output-format/color flags are normalized because actionlint consumes ShellCheck
JSON; use `format` for presentation. Help/version and filename operands prevent
analysis and are rejected. The versioned directive schema describes supported
settings; it does not pin the executable. [Source following and custom shells](config.md#source-resolution)
remain available, including readable files outside the repository.

## Input/output reference

All inputs are optional. Booleans accept `true`/`false`.

| Input                                                                      | Default        | Meaning                                                                    |
| -------------------------------------------------------------------------- | -------------- | -------------------------------------------------------------------------- |
| `files`                                                                    | discover       | Literal newline-separated workflow paths.                                  |
| `format`                                                                   | `github`       | `github`, `default`, `oneline`, `json`, `json-lines`, `markdown`, `sarif`. |
| `ignore`                                                                   | empty          | Newline-separated regular expressions.                                     |
| `config-file`                                                              | discover       | Explicit config file.                                                      |
| `config`                                                                   | inherit        | Inline YAML/JSON overlay.                                                  |
| `working-directory`                                                        | `.`            | Analysis directory.                                                        |
| `shellcheck`, `pyflakes`                                                   | `true`         | Permit external analysis.                                                  |
| `shellcheck-rc`                                                            | inherit        | Rc selection: `true`, `false`, or path.                                    |
| `shellcheck-args`                                                          | `[]`           | Literal argument array.                                                    |
| `add-actionlint-to-path`, `add-shellcheck-to-path`, `add-pyflakes-to-path` | `true`         | Independent exports for later steps.                                       |
| `output-file`                                                              | empty          | Workspace-relative legacy-format report.                                   |
| `fail-on-error`                                                            | `true`         | Fail for findings; invalid options/failures always fail.                   |
| `annotations`                                                              | `auto`         | Finding annotation control.                                                |
| `summary`                                                                  | `true`         | Compact job summary.                                                       |
| `sarif`                                                                    | `false`        | Write additional SARIF file.                                               |
| `review`                                                                   | `false`        | Advisory PR review posting.                                                |
| `token`                                                                    | `github.token` | Used only by the review reporter.                                          |

| Output           | Meaning                                                                             |
| ---------------- | ----------------------------------------------------------------------------------- |
| `exit-code`      | Analysis status: 0 clean, 1 findings, 2 invalid input, 3 incomplete/failure.        |
| `result`         | `success`, `problems-found`, `invalid-options`, `failure`.                          |
| `problems-found` | Whether analysis completed with findings. Partial findings remain in `result-file`. |
| `problem-count`  | Count, or empty when analysis did not complete.                                     |
| `output`         | Complete legacy-format output; prefer files for large reports.                      |
| `output-file`    | Workspace-relative requested path, or empty.                                        |
| `result-file`    | Absolute versioned JSON path, including failure results when writable.              |
| `report-sarif`   | Absolute SARIF path, or empty when not requested/not completed.                     |

## Migration and troubleshooting

The nine shipped inputs and six shipped outputs retain their contracts. New
defaults add a compact summary and independent PATH exports; turn each off when
unwanted. JavaScript removes Docker's Linux/daemon requirement. Existing immutable
Docker releases and digest pins remain untouched. Compatibility images continue
as `action-X.Y.Z`, `action-vX.Y`, `action-vX`, and `action-latest`, using a thin
wrapper around the ordinary binary. [Release maintainers](../scripts/bump-version/README.md)
prepare, inspect and promote the same tested candidate.

| Symptom                    | Check                                                                                                |
| -------------------------- | ---------------------------------------------------------------------------------------------------- |
| No project/workflows found | Checkout first; check `.github/workflows` spelling and `working-directory`.                          |
| Unexpected config          | Read the selected-file log; `.yaml` precedes `.yml`; inspect with `actionlint config show --origin`. |
| Ignore does nothing        | Remove literal outer quotes from multiline `ignore`; use a config YAML list for quoted scalars.      |
| Missing Python             | Install Python before the Action or set `pyflakes: false`.                                           |
| Missing report             | Use `if: always()` in later steps; inspect setup/file-write errors.                                  |
| No review comments         | Read aggregate skip reasons; check event, changed lines and token permission.                        |

For a standalone binary or CLI Docker image, see [manual installation](usage.md#manual-installation).
