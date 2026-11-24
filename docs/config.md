# Configuration

Configuration is optional. actionlint reads the first of `actionlint.yaml`, `actionlint.yml`, `actionlint.json`,
`actionlint.jsonc` or `actionlint.toml` in `.github/`. Select another file with
`actionlint --config path/to/actionlint.toml`.

`actionlint config init --toml` creates `.github/actionlint.toml`. `--json`, `--jsonc`, `--yaml` and `--yml` select other formats, and YAML is the
default. Both `.json` and `.jsonc` files accept comments and trailing commas.

## Configuration file

```yaml
# yaml-language-server: $schema=https://cdn.jsdelivr.net/gh/kjanat/actionlint@2/actionlint.schema.json
repository:
  runners: [linux.2xlarge, windows-latest-xl]
  variables: { DEFAULT_RUNNER: null, ENVIRONMENT_STAGE: "production" }
  secrets: [DEPLOY_TOKEN]
  default-workflow-permissions: read
lint:
  message-ignores:
    ".github/workflows/**/*.{yml,yaml}":
      - "SC2086:"
```

| Key                                       | Purpose                                                                                                                                             |
| ----------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| `plugins`                                 | Exact registry releases, such as `owner/repo@v1.2.3`, or digest-pinned direct artifacts. See [Plugins](plugins.md#install-from-the-registry).       |
| `repository.runners`                      | Additional runner labels. Supports [`path.Match` patterns][pat]. `[]` accepts only built-in labels.                                                 |
| `repository.service`                      | `github.com`, `github-data-residency`, `ghes`, or `{kind = "ghes", version = "3.19"}`. See [runner support](plugins.md#dated-runner-support).       |
| `repository.variables`                    | Variable names mapped to optional string values. A null value keeps the name known.                                                                 |
| `repository.secrets`                      | Secret names, case-insensitive. `[]` permits only built-in and declared `workflow_call` secrets.                                                    |
| `repository.default-workflow-permissions` | `read` or `write`. Neither value grants `id-token`.                                                                                                 |
| `repository.environments`                 | Deployment environments with their variable and secret names.                                                                                       |
| `lint.message-ignores`                    | Repository-relative [glob patterns][doublestar] mapped to regular expressions that match the message or `CODE: message`.                            |
| `lint.lifecycle.policies`                 | Named policies that set notification dates and severities for lifecycle events. See [lifecycle policies](usage.md#lifecycle-notification-policies). |

[`actionlint config migrate`](config-migration.md) previews or writes the current syntax for older root settings.

### JSON and TOML

Every format accepts the same keys, and `extends` entries may name files of any format. A JSON file may carry a
top-level `$schema` key. TOML editors read the schema from a `#:schema` comment.

```toml
#:schema https://cdn.jsdelivr.net/gh/kjanat/actionlint@2/actionlint.schema.json

[repository]
runners                      = ["linux.2xlarge", "windows-latest-xl"]
secrets                      = ["DEPLOY_TOKEN"]
default-workflow-permissions = "read"

[repository.variables]
ENVIRONMENT_STAGE = "production"
```

The `@2` schema URL follows v2. Use `@2.0.0` for an exact release or `@1` for v1 configurations.

The schema rejects unknown keys.

## Extending configuration

Use `extends` to share local files or HTTPS configuration:

```yaml
extends:
  - https://example.org/actionlint/base.yml
  - ../config/team.yml
files:
  includes: [".github/workflows/**/*.{yml,yaml}"]
  excludes: [".github/workflows/generated/**"]
```

Relative references resolve against the declaring file's directory or URL. actionlint applies bases in order, and the
declaring file wins. Maps merge recursively.

Remote downloads require HTTPS and a public address, and they reject credentials and redirects. Each document has a
4 MiB limit and a 30-second timeout. Remote configuration cannot extend local files. Relative executable, module and rc
paths require a local configuration. Missing bases, cycles and chains deeper than 32 levels are errors. The Action's
inline `config` input cannot use `extends`.

## CLI reporting defaults

```yaml
output:
  formats: [github, pretty]
  log-level: info
  summary: true
  network-stats: false
  quiet: false
```

actionlint reads `output` from `--config` or from the repository that contains the current directory. `--no-config`
disables these defaults too.

## Network

Configure fallback services for remote action and reusable workflow files:

```toml
[network.metadata-mirrors]
services                   = ["jsdelivr"]
allow-unknown-repositories = false
```

By default, actionlint tries jsDelivr after a raw-file failure only for repositories it knows are public. Other lookups
fall back to GitHub REST. `services = []` disables mirrors. `allow-unknown-repositories = true` also sends repositories
of unknown visibility to jsDelivr. That setting discloses the repository name, commit hash and file path to jsDelivr.

### Shared defaults and restrictions

A `network-policy.toml` file sets mirror policy across projects. The native CLI and language server read these files in
order:

1. `/etc/actionlint/network-policy.toml` on Linux and macOS, or
   `%ProgramData%\actionlint\network-policy.toml` on Windows.
2. The user's configuration directory: `$XDG_CONFIG_HOME/actionlint/network-policy.toml`
   on Linux, with `$HOME/.config` as the default, `$HOME/Library/Application Support/actionlint/network-policy.toml`
   on macOS, or `%AppData%\actionlint\network-policy.toml` on Windows.
3. The file path or HTTPS URL in `ACTIONLINT_NETWORK_POLICY`, when set.

An invalid policy file stops the analysis. So does a missing file named by `ACTIONLINT_NETWORK_POLICY`. Policy files
support TOML, YAML, JSON and JSONC. Store them outside the project.

```toml
#:schema https://cdn.jsdelivr.net/gh/kjanat/actionlint@master/schemas/policy/network.schema.json

extends = ["https://settings.example.com/actionlint/company.toml"]

[defaults.network.metadata-mirrors]
services                   = ["jsdelivr"]
allow-unknown-repositories = false

[restrictions.network.metadata-mirrors]
require-public       = true
exclude-repositories = [
  "github.com/company/private-*",
  "github.com/company/acquisitions"
]
```

Projects can override `defaults`, and later policy files override earlier ones. Every restriction applies, including
inherited ones. Empty arrays, `false` and `null` cannot remove a restriction. Repository exclusions are
case-insensitive globs that match `host/owner/repository`. `require-public = true` forbids mirror requests for
repositories of unknown visibility. `exclude-services = ["jsdelivr"]` prohibits jsDelivr.

actionlint stops and names the policy file when project settings or flags violate a restriction. Policy files also
apply with `--no-config`. `actionlint config show --origin` shows the file that supplied each mirror setting.

## File selection

Top-level `files` selects the files that actionlint analyzes. Patterns are doublestar globs that use `/`. They are
relative to the repository root, or to the working directory outside a repository.

Omitted or `null` `includes` selects every file, and `includes: []` selects none. `excludes` and include patterns that
start with `!` win regardless of order. actionlint checks a literally named file even when excluded. `--no-ignore`
scans without Git ignore rules or these filters. actionlint can still read excluded files as dependencies of included
workflows.

Use `overrides` to change settings for selected files:

```yaml
overrides:
  - includes: [".github/workflows/release*.yml"]
    excludes: [".github/workflows/release-preview.yml"]
    lint:
      rules:
        security:
          unpinned-reference: error
  - includes: [".github/workflows/legacy*.yml"]
    tools:
      shellcheck:
        config:
          disable: [SC2086]
```

Every matching entry applies in order, and later values win. Each entry needs a positive include pattern, and its
exclusions affect only that entry. Absolute paths and parent traversal are errors.

## Lint rules

```yaml
lint:
  enabled: true
  rules:
    preset: recommended
    correctness:
      deprecated-commands: warn
    conventions:
      job-timeout:
        level: error
        options: { max-minutes: 30 }
```

Every rule accepts `off`, `on`, `info`, `warn` or `error`. `on` uses the rule's native severity, and `default` resets
the rule to its preset or group baseline. `true` and `false` are aliases for `on` and `off`. The object form requires
`level` and accepts `options` where the rule supports them. `options: null` resets the options.

`preset` accepts `recommended`, `all` or `none`, globally or per group. `all` selects every stable rule. A group also
accepts a level, such as `correctness: warn`. `lint.enabled: false` suppresses all lint diagnostics.

Settings apply in this order, and later ones win: rule defaults, global preset, CLI presets, group preset or level, rule
settings, then explicit exclusions. `--strict` enables stable `correctness`, `security` and `suspicious` rules. Select
`conventions` rules individually or with `preset: all`. An explicit `off` survives presets and `--strict`.

Nursery rules are experimental. Presets, group levels and `--strict` never select them. Enable one by setting its level,
or enable all with `--experimental`. `--experimental=false` disables them all.

### Findings in dependencies

actionlint also checks downloaded composite actions. Their findings stay visible and do not fail the check. Errors that
guarantee an execution failure still count, such as invalid expression syntax.

To fail on security findings and selected diagnostics in dependencies:

```toml
[lint.dependencies]
fail-on-categories = ["security"]
fail-on-rules      = ["script-interpolation", "shellcheck/SC2086"]
```

`fail-on-all = true` includes every enabled dependency finding. A rule name covers all its codes, and a `rule/code` entry
covers one code. `lint.rules` still decides enablement and severity. Lists replace inherited lists. JSON output marks
findings that do not fail the check with `non_blocking: true`.

## Ruff

Ruff checks scripts with `shell: python`, `shell: python3`, or a custom command that starts with either interpreter. It
covers workflows and local composite actions. See
[plugin installation](plugins.md#install-from-the-registry).

```yaml
tools:
  ruff:
    enabled: true
    config:
      target-version: py314
      lint:
        extend-select: [B, S]
        ignore: []
```

Ruff validates `config` against its native options. The flat `target-version`, `select` and `ignore` keys are shorthands
for `config.target-version`, `config.lint.select` and `config.lint.ignore`. Without a configured target, actionlint
infers one from a versioned interpreter such as `python3.9`.

Ruff runs read-only. It ignores repository Ruff configuration, caches, automatic fixes and `noqa` comments. Use rule
levels, overrides and YAML suppressions for exceptions. Ruff skips a script when an unquoted expression forms part of a
Python token or statement, such as `item_${{ matrix.os }}` or `import ${{ inputs.module }}`.

## ShellCheck

```yaml
tools:
  shellcheck:
    enabled: true
    config:
      disable: [SC2086]
      enable: [quote-safe-variables]
      external-sources: true
      source-path: [scripts]
```

`tools: {shellcheck: false}` disables ShellCheck.

The Wasm plugin supports individual disabled codes and optional check names. Rc files, code ranges and `all` require a
native executable. Select one with `tools.shellcheck.backend.process` or `--tool shellcheck=/path/to/shellcheck`.

`config` accepts inline settings, an rc-file path, or a directory where actionlint searches for `.shellcheckrc`, then
`shellcheckrc`. actionlint discovers no rc file otherwise.

```yaml
tools:
  shellcheck:
    config: ./.shellcheckrc # Relative to actionlint.yaml.
```

| Inline key          | Accepted values                                                       |
| ------------------- | --------------------------------------------------------------------- |
| `disable`           | List of codes, ranges such as `SC3000-SC4000`, or `all`.              |
| `enable`            | List of optional check names, or `all`. `[]` clears inherited checks. |
| `shell`             | `sh`, `bash`, `dash`, `ksh`, or `busybox`.                            |
| `extended-analysis` | Boolean; omission keeps ShellCheck's default.                         |
| `external-sources`  | Boolean; defaults to enabled in actionlint.                           |
| `source-path`       | List of directories searched for sourced files.                       |

actionlint's built-in ShellCheck exclusions still apply.

Rc paths in an inline overlay resolve from the analysis working directory. Inherited paths keep their configuration
file's directory. Config paths and inline `source-path` entries accept these substitutions:

| Expression                  | Meaning                                                                                                                                                      |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `${{ configdir }}`          | Directory containing the selected actionlint configuration; also the base for relative rc paths. Falls back to the repository root, then analysis directory. |
| `${{ gitdir }}`             | Analyzed repository root; falls back to the analysis directory when no project is found.                                                                     |
| `${{ github.workspace }}`   | `GITHUB_WORKSPACE`, or the local repository root.                                                                                                            |
| `${{ github.action_path }}` | Analyzed composite action's directory; unavailable for ordinary workflow steps.                                                                              |

Unknown variables are configuration errors. GitHub evaluates expressions in workflow inputs first. Pass a literal there
with `${{ '${{ configdir }}/.shellcheckrc' }}`.

Inline settings follow the [ShellCheck 0.11.0 schema][shellcheck-schema]. Optional checks depend on the installed
ShellCheck version. See the [ShellCheck manual][shellcheck-manual] for native settings.

`actionlint --log-level debug` shows each script's dialect, working directory, rc arguments, directives and options.

### Script selection and directives

ShellCheck checks recognized shell steps, including templates such as `bash -euxo pipefail {0}`, interpreter paths and
`.exe` names. Unknown wrappers and templates that use `-c` or `-s` need a leading directive:

```yaml
- shell: custom-shell {0}
  run: |
    # shellcheck shell=bash
    echo "$HOME"
```

The directive selects the analysis dialect. GitHub's runtime interpreter stays unchanged. Dialect precedence runs from
highest to lowest:

1. ShellCheck command arguments, then `SHELLCHECK_OPTS`.
2. A leading `# shellcheck shell=...` directive.
3. Application settings, then inline `config.shell`.
4. The workflow's resolved shell, ahead of any shebang.

### Source resolution

Sourced files and relative `source-path` entries resolve from the step's working directory. actionlint takes it from the
step `working-directory`, the job default, the workflow default, then the workspace root. For `working-directory: app`
and `source-path: [scripts]`, ShellCheck searches `app/scripts`. A dynamic or missing working directory limits analysis
to the embedded script.

The Wasm plugin follows sources when `external-sources: true` or `source-path` enables access. Workflow directives cannot add grants, and symlinks cannot escape
them. One invocation may read 8 MiB of source from 256 files, with 4096 lookups and 32 search directories. Exceeding a
limit makes the analysis incomplete.

Local composite actions use their own shell and working directory. Their relative working directories start at the
workspace root.

## Tool execution backends

Each `tools` key names a bundled tool provider, `ruff` or `shellcheck`, or a provider from a plugin. Every entry accepts
`enabled`, `backend` and `config`, and `true` or `false` as shorthand for `enabled`. A string `config` selects a
configuration file for tools that read one, such as ShellCheck.

The published schema knows only the bundled providers. `actionlint config schema` prints a schema that adds the
providers from `--plugin` or `plugins`, including their shorthand keys. Save the output and point the editor directive
at the copy.

`backend` selects how the engine runs. `process` runs a native executable, and `wasm` loads an engine in-process.

```yaml
tools:
  shellcheck:
    backend:
      process:
        executable: mise
        args: [exec, --silent, --quiet, "github:kjanat/shellcheck-rs@latest", --, rshellcheck]
        env: { SHELLCHECK_OPTS: "" }
    config:
      enable: all
  ruff:
    backend:
      wasm:
        path: ./tools/plugin.component.wasm.gz
    config:
      lint: { extend-select: [B, S] }
```

Relative executable paths resolve from the declaring configuration file. Bare names use `PATH`. actionlint passes `args`
literally before the engine invocation and never runs them through a shell. `env` affects only the child process.

Repository-selected executables require `--allow-config-processes`. Enable it only for trusted configuration, including
inherited files. Without the flag, actionlint reports an error before it launches the process.

`--no-tool ruff` disables Ruff. `--tool ruff` selects `ruff` on `PATH`, `--tool ruff=/path/to/ruff` selects a binary,
and `--tool ruff='python -m ruff'` runs the Python module. Without a plugin or explicit backend, actionlint searches
`PATH` and skips Ruff when it is missing.

See [script engines](plugins.md#script-engines).

## Policy checks

`security.unpinned-reference` requires full commit SHAs for actions and reusable workflows, and digests for `docker://`
images. It skips local references. `exclude` lists exceptions:

```yaml
lint:
  rules:
    security:
      unpinned-reference:
        level: true
        options:
          exclude: ["my-org/*"]
```

Patterns without `@` match the repository or action path, including its sub-actions and reusable workflows. Patterns
with `@` match the whole reference. Include `docker://` to match container references.

`conventions.job-timeout` requires `timeout-minutes` on jobs that run steps. Its options set inclusive bounds:

```yaml
lint:
  rules:
    conventions:
      job-timeout:
        level: on
        options: { min-minutes: 5, max-minutes: 60 }
```

`security.missing-permissions` is recommended. By default, a workflow-level `permissions` key or one on every job
satisfies it, and it skips workflows triggered only by `workflow_call`. `require-at` accepts `workflow`, `every-job`, or
both as an array. With `require-at`, the rule also checks `workflow_call` workflows.

```yaml
lint:
  rules:
    security:
      missing-permissions:
        level: error
        options: { require-at: every-job }
```

### forbidden-suppression

`conventions.forbidden-suppression: on` prohibits inline and configured exceptions for every rule. `options.rules`
limits it to the listed rules.

```yaml
lint:
  rules:
    conventions:
      forbidden-suppression:
        level: on
        options:
          rules: [cache-call-unrestricted, cache-write-untrusted]
          report: all
```

| `report`        | Prohibited directive | Original finding |
| --------------- | -------------------- | ---------------- |
| `all` (default) | Reported             | Reported         |
| `suppression`   | Reported             | Suppressed       |
| `violation`     | Not reported         | Reported         |

The policy cannot exempt itself or override CLI and path ignores.

## Local action source mappings

Map a generated action directory to its checked-in source:

```yaml
local-actions:
  ./generated-action: ./actions/source
```

actionlint reads the action metadata from the source directory. Both paths are relative to the repository root. A
missing source is an error. Overrides can give different workflows different mappings.

Local action lookup tries `action.yml`, `action.yaml`, `Dockerfile`, then `dockerfile`. actionlint accepts any input for
a Dockerfile-only action.

## Reusable ignores

Declare repeated exceptions once:

```yaml
ignores:
  - rule: missing-local-action
    uses: ./generated-action
    file: .github/workflows/ci.yml
    reason: Release bundle built during CI.
```

Each entry names a `rule` and at least one of `file`, `uses`, `job` or `step`. Every selector must match. `file` takes
repository-relative globs. `uses`, `job` and `step` match exact values, and `step` accepts an ID or a name. Inside a
composite action, `job` matches the calling workflow job. A supplied `ignores` list replaces inherited entries, and
`ignores: []` clears them.

## Inline diagnostic suppressions

```yaml
cache-mode: write # actionlint:ignore cache-write-untrusted -- reviewed default-branch code only
```

`# actionlint:ignore-next-line RULE -- reason` on the line before also works. The `-- reason` suffix is optional. These
spellings are equivalent:

```yaml
- run: echo ok # actionlint:ignore expression,shellcheck
- run: echo ok # actionlint-ignore expression shellcheck
- run: echo ok # ignore expression shellcheck
- run: echo ok # ignore[expression, shellcheck]
```

Separate rule IDs with commas or whitespace. Supply at least one ID from `actionlint rules`. A directive on a block
scalar header such as `run: |` covers the body. Blank lines or other comments detach an `ignore-next-line` directive.

For ShellCheck findings in sourced scripts, put the directive on the `run` line or its block header. Comments inside
sourced scripts suppress nothing.

Malformed directives report `inline-suppression`. To suppress a rule in a whole file, use `lint.rules.disable` in an
[override](#file-selection). Configuration, I/O and tool-launch failures cannot be suppressed.

---

[Checks](checks.md) | [Installation](install.md) | [Usage](usage.md) | [Integration interfaces](api.md) | [References](reference.md)

[pat]: https://pkg.go.dev/path#Match
[doublestar]: https://github.com/bmatcuk/doublestar
[shellcheck-schema]: ../schemas/shellcheck/0.11.0.schema.json
[shellcheck-manual]: https://github.com/koalaman/shellcheck/blob/v0.11.0/shellcheck.1.md
