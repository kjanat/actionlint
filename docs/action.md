# GitHub Action

[![Latest release](https://img.shields.io/github/v/release/kjanat/actionlint?logo=github&logoColor=white&label=release&labelColor=181717&color=black)](https://github.com/kjanat/actionlint/releases)

The upcoming v2 Node 24 Action runs the ordinary actionlint binary without Docker.
The examples target v2, which is not published yet. Existing immutable v1 releases
keep their original implementation.

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

This discovers workflows, reports findings as annotations, and writes a compact
job summary. Findings fail the step. Pin a **published release commit SHA** or
normal `vX.Y.Z` tag for immutable consumption; major/minor tags move.

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

`install-only` exports the selected binary for later steps without reading workflows
or lint configuration. Keep `add-actionlint-to-path: true`
(the default). Analysis and reporting inputs do not apply in this mode. This installs only the actionlint binary:
Bundled tools such as ShellCheck and Ruff get no plugins or native executables in install-only mode. Provision native tools
separately if later steps need them; configure and pin Wasm plugins for later actionlint checks.

Use `version-file: .tool-versions` instead of `version` to read a checked-in pin:

```text
nodejs 24.21.0
actionlint 1.17.0
```

An ordinary file containing only `1.17.0` is also supported. Paths resolve from
`working-directory`; `.tool-versions` comments and other tools are ignored. The
actionlint entry must select one exact release. Ranges, `system`, and source/path
selectors are not supported. An explicit `version` takes precedence without reading
`version-file`. With neither input, the binary matches the Action release. An optional
`v` prefix is accepted; the `version` output contains the selected version without it.

Install-only works with older releases. Automatic linting requires the v2 Wasm
plugin protocol, even with `plugin-cache: false`. Unsupported binaries fail before
analysis. Use the matching v1 Action to lint with an older release, or install and
invoke its binary directly as shown above.

After setup, `result` is `success` and `exit-code` is `0`. No analysis occurred:
`problem-count`, `output`, `output-file`, `result-file`, and `report-sarif` are empty.

## Configuration

Keep reusable settings in `.github/actionlint.yaml`, shared with the CLI:

```yaml
plugins: [shellcheck, ruff]
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

The quick-start workflow needs no changes. `.yaml` wins over `.yml` if both
exist. No config means defaults; an explicit missing `config-file` is an error.
The log identifies the selected file and any inline override.

Use `config` for an invocation-specific overlay:

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

Or pass a repository variable containing YAML/JSON directly:

```yaml
- uses: kjanat/actionlint@v2
  with:
    config: ${{ vars.ACTIONLINT_CONFIG }}
```

Use `toJSON(...)` for object-valued expressions. Pass YAML/JSON strings directly.
The real config file offers editor completion, hover and
validation; editors generally treat `with.config` as a string.

### Precedence and resets

The overlay merges over the selected file. Maps merge recursively so changing one
tool option preserves its neighbors; lists and scalars replace so removals are
unambiguous. These examples start with expression-reference `options.variables: [DEPLOY_ENV]` and
`tools.shellcheck.config: {disable: [SC2086], enable: [all]}`:

| Overlay                                                  | Effect                                            |
| -------------------------------------------------------- | ------------------------------------------------- |
| Omitted or blank input                                   | Inherit the file.                                 |
| `config: '{}'`                                           | Keep inherited settings.                          |
| `config: 'null'`                                         | Reset the complete config to defaults.            |
| `config: 'tools: {shellcheck: false}'`                   | Set only ShellCheck's enabled flag to false.      |
| `config: 'tools: {shellcheck: {config: {disable: []}}}'` | Clear the disabled-code list.                     |
| `config: 'tools: {shellcheck: {config: .shellcheckrc}}'` | Replace the inline directive map with an rc path. |
| `config: 'tools: {shellcheck: {config: null}}'`          | Clear rc/inline configuration.                    |

An empty map inherits **a map**. Switching between an rc string and an inline
mapping replaces the old form; `{}` after an rc string selects an empty mapping.
Unknown inline keys fail with their location. Historically ignored file keys warn.
For `lint.rules.correctness.expression-reference.options.variables`, `[]` allows
no configured variables; `null` restores the default of no variable-name checking.

`disable-tools: shellcheck` disables ShellCheck. Leaving ShellCheck out of
`disable-tools` permits the tool; it does not override `tools.shellcheck: false`.
Configure plugin options in `tools.shellcheck.config`; see [plugin configuration](plugins.md).

### Path bases

| Value                             | Relative to                                                           |
| --------------------------------- | --------------------------------------------------------------------- |
| `working-directory`               | `GITHUB_WORKSPACE`; absolute paths also work.                         |
| `files`, `config-file`            | Action `working-directory`.                                           |
| Rc path in the config file        | Directory containing that config file.                                |
| Rc path supplied through `config` | Action `working-directory`; inherited paths keep their original base. |
| `${{ configdir }}` in config      | Selected config file's directory.                                     |
| Inline `source-path`              | Analyzed step's effective working directory.                          |
| `output-file`                     | `GITHUB_WORKSPACE`, for compatibility.                                |

For a checkout at `app`, use `working-directory: app` and
`config-file: .github/actionlint.yaml`; `output-file: reports/lint.json` still
writes under the workspace's `reports/`, outside `app`.

Paths are literal; `files` is newline-separated and does not expand globs.
`ignore` is newline-separated regex text: quotes inside `ignore: |` are literal.
Config interpolation supports the [listed path contexts](config.md#shellcheck).
GitHub evaluates expressions in workflow inputs
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

Finding annotations include the diagnostic message followed by a numbered source
excerpt and caret range when source is available. Embedded line breaks are encoded
for the runner, so each finding emits one annotation with a multiline body.

Maintainers can inspect GitHub's rendering with the manual **Annotation preview**
workflow once it is merged into the default branch. In Actions, select that workflow, choose **Run workflow** and select the
branch to build. It emits one intentional error, warning and notice in separate
jobs, each with its own summary. Expected findings do not fail the job. The preview never runs on
pushes or pull requests; ordinary CI checks annotation payloads in captured output.

The job summary shows findings with source
excerpts, caret ranges and two surrounding lines on either side when the source is available.
Documentation URLs appear on their own line. Source locations link to the
run's commit only when the checkout matches its repository and revision and the file
matches the committed content, allowing LF/CRLF line-ending conversion. Other content changes,
generated, external and nested-checkout files have plain-text locations. Repository-defined Git filters are not run.
Locations respect `working-directory`.
The summary displays findings and identifies incomplete analysis. For multiple invocations in one job,
set `summary-title: Production workflows` to add a small report label. Summaries have no title by default.
Clean runs, empty selections and failures include a short status message in the summary and text log.

Save reports even when findings fail the lint step:

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

`result-file` contains a [versioned result](../schemas/results/v1.schema.json):

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

CLI `check --json`, Action `format: json`, and `result-file` share this contract.
JSON Lines uses the same diagnostic objects with `schema_version` on each record.
See [the result contract](results.md) for TypeScript types, schema evolution, and
migration from the previous Action diagnostic array.

Files use unique absolute paths and survive for later steps in the **same job**.
Upload them for use elsewhere. A result is attempted for setup/input failures too;
failure to create/write files can prevent that output. Missing output is never
evidence of a clean run. Incomplete results include collected diagnostics; `file_count`
may be null. SARIF is exposed only for completed analysis. `fail-on-error: false`
allows a step with findings to pass while preserving the recorded analysis outcome.
Match findings by path, location and rule; array indexes can change between runs.

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

Reviews group findings on changed lines and add applicable suggestions. Reruns
deduplicate existing comments. Missing PR context, unchanged lines, mismatched
source, limits and permission errors produce feedback. Use the ordinary `pull_request` event; fork PR tokens may lack
write permission. Review failure is advisory and leaves the analysis outcome intact.

## Plugins and binary installation

Each invocation downloads the selected actionlint binary, verifies its checksum,
and executes it. `add-actionlint-to-path` makes that binary available in later
steps. `install-only: true` performs only this installation.

Declare ShellCheck and Ruff Wasm plugins in the project's `plugins` list and
commit the generated lockfile. The Action uses the same pinned artifacts as the
CLI. `disable-tools` lists tool ids to turn off, such as `shellcheck` or `ruff`;
those tools run neither natively nor as plugins, and a locked plugin whose name or
alias is a listed id is not downloaded. An id that names no bundled tool and no
plugin fails with the list of valid ids. Plugin options belong in project configuration.
See [installing and configuring plugins](plugins.md).

`plugin-cache: true` restores and saves pinned Wasm artifacts through GitHub
Actions cache. The Action reads the plugin plan before restoring the cache;
analysis then hydrates missing artifacts and verifies their digests. The cache
never contains plugin declarations, lockfiles, or the actionlint executable.
After automatic config migration, a second read-only plan includes any newly
locked defaults when saving the cache.

Node 24 support is required from the runner. Linux, macOS and Windows x64/arm64
are supported where the selected release supplies an asset. Binary downloads
require network access, including reruns. HTTP(S) proxy settings are honored.

## Input/output reference

All inputs are optional. Booleans accept `true`/`false`.

| Input                            | Default        | Meaning                                                                                                                               |
| -------------------------------- | -------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| `version`                        | Action release | Exact binary release; overrides `version-file`.                                                                                       |
| `version-file`                   | empty          | Exact version file or `.tool-versions`, relative to `working-directory`.                                                              |
| `install-only`                   | `false`        | Install actionlint on PATH without analysis or reporting.                                                                             |
| `plugin-cache`                   | `true`         | Restore and save digest-verified plugin artifacts.                                                                                    |
| `files`                          | discover       | Literal newline-separated workflow paths.                                                                                             |
| `format`                         | `github`       | `github`, `default`, `oneline`, `json`, `json-lines`, `markdown`, `sarif`.                                                            |
| `ignore`                         | empty          | Newline-separated regular expressions.                                                                                                |
| `config-file`                    | discover       | Explicit config file.                                                                                                                 |
| `config`                         | inherit        | Inline YAML/JSON overlay.                                                                                                             |
| `working-directory`              | `.`            | Analysis directory.                                                                                                                   |
| `disable-tools`                  | empty          | Tool ids to turn off, separated by newlines or commas, such as `shellcheck` or `ruff`.                                                |
| `shellcheck`, `ruff`, `pyflakes` | none           | Ignored. List the tool id in `disable-tools` to turn a tool off.                                                                      |
| `add-actionlint-to-path`         | `true`         | Export actionlint for later steps.                                                                                                    |
| `output-file`                    | empty          | Workspace-relative legacy-format report.                                                                                              |
| `fail-on-error`                  | `true`         | Fail for findings; invalid options/failures always fail.                                                                              |
| `annotations`                    | `auto`         | Finding annotation control.                                                                                                           |
| `summary`                        | `true`         | Compact job summary.                                                                                                                  |
| `summary-title`                  | Empty          | Add a summary label and text-log label, for example `Production workflows`. Without one, only text logs use the Action invocation ID. |
| `sarif`                          | `false`        | Write additional SARIF file.                                                                                                          |
| `review`                         | `false`        | Advisory PR review posting.                                                                                                           |
| `token`                          | `github.token` | Used only by the review reporter.                                                                                                     |

| Output           | Meaning                                                                                                                 |
| ---------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `version`        | Installed binary version, without the `v` prefix.                                                                       |
| `exit-code`      | Exit status: 0 successful setup/clean analysis, 1 findings, 2 invalid input, 3 failure.                                 |
| `result`         | `success`, `problems-found`, `invalid-options`, `failure`.                                                              |
| `problems-found` | Whether analysis completed with findings. See `result-file` for partial results.                                        |
| `problem-count`  | Count, or empty when analysis did not complete.                                                                         |
| `output`         | Selected serialization; JSON uses the same result as `result-file`, including failures. Prefer files for large reports. |
| `output-file`    | Workspace-relative requested path, or empty.                                                                            |
| `result-file`    | Absolute versioned JSON path, including failure results when writable.                                                  |
| `report-sarif`   | Absolute SARIF path, or empty when not requested/not completed.                                                         |

## Migration and troubleshooting

The inputs `shellcheck`, `ruff` and `pyflakes` are ignored. GitHub shows their
deprecation message when a workflow sets them. To turn a tool off, list its id in
`disable-tools`, for example `disable-tools: shellcheck`.
**JSON and JSON Lines change shape**:
JSON now returns the versioned result object instead of a diagnostic array; JSON
Lines uses versioned canonical diagnostics. Update consumers using the
[migration guide](results.md) before upgrading. The unified contract is
the default; there is no separate legacy JSON mode. Coordinate affected consumers
before promoting moving Action tags.

New defaults add a compact summary and actionlint PATH export; turn either off when
unwanted. JavaScript removes Docker's Linux/daemon requirement. Compatibility images are published
as `action-X.Y.Z`, `action-vX.Y`, `action-vX`, and `action-latest`, using a thin
wrapper around the ordinary binary. [Release maintainers](../tools/release/bump-version/README.md)
prepare, inspect and promote the same tested candidate.

| Symptom                    | Check                                                                                                |
| -------------------------- | ---------------------------------------------------------------------------------------------------- |
| No project/workflows found | Checkout first; check `.github/workflows` spelling and `working-directory`.                          |
| Unexpected config          | Read the selected-file log; `.yaml` precedes `.yml`; inspect with `actionlint config show --origin`. |
| Ignore does nothing        | Remove literal outer quotes from multiline `ignore`; use a config YAML list for quoted scalars.      |
| Missing report             | Use `if: always()` in later steps; inspect setup/file-write errors.                                  |
| No review comments         | Read aggregate skip reasons; check event, changed lines and token permission.                        |

For a standalone binary or CLI Docker image, see [manual installation](usage.md#manual-installation).
