# Configuration

Configuration is optional. Put `actionlint.yaml`, `actionlint.yml`, `actionlint.json` or
`actionlint.toml` in `.github/`, or select a file with `actionlint --config path/to/actionlint.toml`.
actionlint reads the first of those names that exists. The CLI and GitHub Action use the same
settings.

## Configuration file

```yaml
# yaml-language-server: $schema=../node_modules/@kjanat/actionlint/actionlint.schema.json
lint:
  rules:
    correctness:
      runner-label:
        level: default
        options: { labels: [linux.2xlarge, windows-latest-xl] }
      expression-reference:
        level: default
        options:
          variables: [DEFAULT_RUNNER, ENVIRONMENT_STAGE]
          secrets: [DEPLOY_TOKEN]
      workflow-call:
        level: default
        options: { default-permissions: restricted }
  message-ignores:
    ".github/workflows/**/*.{yml,yaml}":
      - "shellcheck reported issue in this script: SC2086:.+"
```

| Key                                                                | Purpose                                                                                                                                                        |
| ------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `plugins`                                                          | CLI plugin dependencies, such as `owner/repo@v1.2.3`; resolved artifacts are pinned in the adjacent lock. See [Plugins](plugins.md#install-from-the-registry). |
| `lint.rules.correctness.runner-label.options.labels`               | Additional runner labels; supports [`path.Match` patterns][pat].                                                                                               |
| `lint.rules.correctness.expression-reference.options.variables`    | Allowed `vars` names. Omitted or `null` disables checking; `[]` allows none.                                                                                   |
| `lint.rules.correctness.expression-reference.options.secrets`      | Allowed secret names, case-insensitive. Omitted or `null` disables checking; `[]` allows only built-in and declared `workflow_call` secrets.                   |
| `lint.rules.correctness.workflow-call.options.default-permissions` | Assumed repository token default: `restricted` (default) or `permissive`. Neither implicitly grants `id-token`.                                                |
| `lint.message-ignores`                                             | Repository-relative [glob patterns][doublestar] mapped to diagnostic message regular expressions.                                                              |

Older root settings are converted at the parsing boundary. Use
[`actionlint config migrate`](config-migration.md) to preview or write the current syntax.

### JSON and TOML

The same keys apply in every format, and `extends` entries may name files of any format. A JSON
file may carry a top-level `$schema` key. TOML editors take the schema from a `#:schema` comment:

```toml
#:schema ../node_modules/@kjanat/actionlint/actionlint.schema.json

[lint.rules.correctness.runner-label]
level = "default"

[lint.rules.correctness.runner-label.options]
labels = ["linux.2xlarge", "windows-latest-xl"]

[lint.rules.correctness.expression-reference]
level = "default"

[lint.rules.correctness.expression-reference.options]
variables = ["DEFAULT_RUNNER", "ENVIRONMENT_STAGE"]
secrets   = ["DEPLOY_TOKEN"]

[lint.message-ignores]
".github/workflows/**/*.{yml,yaml}" = [
  "shellcheck reported issue in this script: SC2086:.+"
]
```

Errors and warnings name the line and column in the file as written.

The [schema](../actionlint.schema.json) provides editor completion and validation.
The YAML language service completes quoted property names after a prefix is
entered. YAML LS 2.0.0 omits completions for empty quoted keys, and accepting its
suggestions inside empty quoted values leaves a trailing quote. Remove the quotes
or enter a prefix before completing these entries. YAML uses nested mappings: `lint.rules.security` is a literal key, not a nested path.
Use TOML when dotted property paths are preferred.
An installed npm package also supplies it at
`../node_modules/@kjanat/actionlint/actionlint.schema.json` from a `.github/` config;
see [schema distribution](../distribution/npm/facade/README.md#configuration-schema).
Without an installed package, select a matching published version from that guide.
Use a source-commit schema when trying unreleased settings; `HEAD` changes with development.
The full editor experience works in a real config file. YAML inside the Action
`config` string is validated at runtime; editors generally see only a string.
The schema rejects unknown keys. Runtime parsing warns about ignored keys at the
top level and in legacy sections, with their locations and accepted alternatives.
Rule options and inline overlays reject unknown keys. Regex and glob
validity is checked when actionlint loads the file.

## Extending configuration

Use `extends` to share local configuration files:

```yaml
extends: [../config/actionlint-base.yml, ../config/team.yml]
files:
  includes: [".github/workflows/**/*.{yml,yaml}"]
  excludes: [".github/workflows/generated/**"]
```

Each path resolves relative to the file declaring it. Bases are applied in order,
then the declaring file wins. Maps merge recursively; arrays and scalar values
replace earlier values. `null` resets the selected setting. Inherited ShellCheck
rc paths resolve relative to the configuration that declares them.

Only local files are supported: no remote downloads, package resolution, or
commands. Missing bases, cycles, invalid base settings, and chains deeper than
32 levels are errors. `config.Load` and file-based CLI configuration resolve
inheritance. In-memory `config.Parse` and inline Action overlays cannot resolve
`extends`; select a configuration file instead.

## File selection

Top-level `files` controls which input files are analyzed, independently of lint
rule settings. Patterns are repository-relative doublestar globs using `/`; when
there is no repository, they are relative to the working directory. They filter
selected inputs and do not discover additional files. Inside a Git repository,
discovery honours Git's ignore rules. Use `files.excludes` for explicit exclusions.

Omitted or `null` `includes` selects every input; `includes: []` selects none.
`excludes` and include patterns starting with `!` exclude matches regardless of
order. A negative-only include list selects nothing. Excluded input files are
not read or linted, even when explicitly passed on the command line. Referenced
actions, reusable workflows, and shell sources can still be read as dependencies
of included workflows. File selection does not restrict dependency reads.

Use `overrides` to change settings for selected files without excluding them:

```yaml
overrides:
  - includes: [".github/workflows/release*.yml"]
    excludes: [".github/workflows/release-preview.yml"]
    lint:
      rules:
        policy:
          require-commit-hash: error
  - includes: [".github/workflows/legacy*.yml"]
    tools:
      shellcheck:
        config:
          disable: [SC2086]
```

Matching overrides merge in order, with later settings winning. Each entry needs
at least one positive include pattern; exclusions affect that entry only.
Each entry can overlay `lint` and `tools`. Tool and rule settings merge independently;
omitted options inherit their values.
External tool provisioning uses each selected file's effective settings.

## Lint rules

The `lint` section owns analysis settings. Rules are grouped by concern:
`correctness`, `suspicious`, `style`, `security`, `policy`, and `external`. The separate
`nursery` group is reserved for rules under development. Optional stable rules
belong to their concern groups. The shared
configuration resolver is used by analysis frontends. Top-level `overrides` provide
file scopes that can also host future formatter settings. Formatting and LSP
options are not implemented yet; do not add placeholder keys for them.

```yaml
lint:
  enabled: true
  rules:
    preset: recommended
    correctness:
      deprecated-commands: warn
    suspicious:
      case-insensitive-conditions: off
    policy:
      require-job-timeout:
        level: error
        options: { max-minutes: 30 }
```

Every catalog rule accepts `off`, `on` (use its native severity), `info`, `warn`,
or `error`. Use `default` to reset a rule or group override to the applicable
preset/group baseline. `true` is an alias for `on`, and `false` for `off`; a group also accepts `all` as shorthand for `{preset: all}`. `options: null` resets a rule's options. Empty strings are invalid. The object form requires `level` and accepts typed `options` only where
the rule supports them. Unknown groups, rules, levels and option keys are rejected.
For example, `require-permissions` accepts `{scope: job}`; `require-job-timeout`
accepts timeout bounds. The generated schema describes each rule's own options.
`actionlint rules --json` exposes each rule's concern, recommended status and
maturity (`stable` or `nursery`) from the same catalog.

Global and group `preset` values are `recommended`, `all` (all stable rules), and
`none`. A group also accepts a level shorthand, e.g. `correctness: warn`, or
`{level: warn, if-cond: error}`. Stable non-recommended rules are included
by `all` or their group level, but not by `recommended`. Nursery rules need
an explicit nursery group/rule selection or `--experimental`. `lint.enabled: false`
suppresses all lint diagnostics. Operational errors fail the analysis independently of rule levels.

Resolution order is: rule defaults, global preset,
CLI presets, group preset/level, individual rule settings, then explicit exclusion
lists. File overrides are merged before that resolution. `--experimental=false`
is an explicit exclusion of nursery rules only. `--strict` selects all stable
rules but respects group and individual exceptions. Legacy `policy` settings
are migrated to explicit levels, so explicit disables now survive presets and
`--strict`. Conflicting old and current values in one document are errors.
Cache safety checks belong to security, with cache-operation under correctness.

Severity is carried through JSON, SARIF and GitHub annotations. As before, the CLI
exits nonzero for any reported finding, including warnings and informational
findings. This change does not silently change CI exit-code behavior.

The compatibility shortcut `disable` accepts any rule ID listed by `actionlint rules`. It suppresses
that rule's errors and warnings without skipping analysis needed by other checks.
Omission, `null` or `[]` suppresses nothing. Unknown rule IDs are rejected.

### Stable suspicious rules

These four rules belong to `lint.rules.suspicious`. They are stable but not
recommended defaults. Enable them individually, set `suspicious: warn` for the
group, select `preset: all`, or use `--strict`. Explicit `off` still wins.

```yaml
lint:
  rules:
    suspicious:
      string-conditions: warn
      mixed-type-comparisons: error
      case-insensitive-conditions: off
      mixed-type-matrix-filters: warn
```

| Rule                          | What it examines                                                                                                                                                                 |
| ----------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `string-conditions`           | Bare known string references in job, step and snapshot conditions. Nonempty `'false'` is truthy.                                                                                 |
| `mixed-type-comparisons`      | Known string references compared with numeric or boolean literals using loose equality/coercion.                                                                                 |
| `case-insensitive-conditions` | Identity, branch, label and environment comparisons that ignore case.                                                                                                            |
| `mixed-type-matrix-filters`   | Different scalar kinds in literal matrix include/exclude filters, including nested values. Boolean, number, null and string are distinct; all YAML numeric forms share one kind. |

Unknown dynamic axes with boolean/numeric filters are also examined. Dynamic
filter entries and wholly dynamic matrices cannot be checked statically.
See [expression behavior](expression-behavior.md) for evidence and limitations.

### Style rules

`lint.rules.style` holds recommended rules for declarations GitHub accepts that
do nothing. They report at warning severity. They used to be syntax errors;
GitHub's own parser accepts each of these documents.

| Rule             | What it examines                                                                    |
| ---------------- | ----------------------------------------------------------------------------------- |
| `empty-triggers` | An empty `on:` section (`on:`, `on: []` or `on: {}`). No event starts the workflow. |
| `empty-job`      | A job without steps, or with `steps: []`. Reusable workflow calls are exempt.       |
| `empty-run`      | A run step whose script is empty.                                                   |

The related `numeric-values` rule belongs to `correctness`. It reports a
fractional `max-parallel`, which GitHub truncates toward zero (`1.5` runs as `1`),
and a `max-parallel` that is not greater than zero.

### Nursery

`lint.rules.nursery` is reserved for rules under development. It uses the same
levels, presets and individual settings as other groups. Global `preset: all`
and `--strict` exclude it; `nursery: {preset: all}` or `--experimental`
selects it. `--experimental=false` disables only nursery rules, never stable
rules. There are currently no nursery rules, so the flag currently adds none.
The former unreleased `lint.rules.experimental` section is rejected; configure
the four stable checks under `suspicious` instead.

### Per-file overrides

```yaml
overrides:
  - includes: [".github/workflows/**/*.{yml,yaml}", "!.github/workflows/release.yml"]
    lint:
      rules:
        suspicious:
          mixed-type-matrix-filters: warn
  - includes: [".github/workflows/legacy.yml"]
    lint:
      rules:
        correctness:
          deprecated-commands: off
        suspicious: off
```

Patterns use repository-relative paths with `/` and [doublestar globs][doublestar].
Without a repository, paths are relative to the analysis working directory.
An entry needs at least one positive pattern. Any matching `!` exclusion wins
within that entry, independent of order. Absolute paths and parent traversal are rejected.
All matching entries apply in order; later values win. Mappings merge, lists
replace, omitted fields inherit, and `null` resets a section to its defaults.
Partial mappings override individual options of group and rule shorthands.
Rule object settings require `level`; omitted option keys inherit.
Each file gets its own effective configuration, without changing other files.

## Ruff

Ruff checks Python scripts selected by `shell: python`, `shell: python3`, or a
custom command beginning with either interpreter, including workflow/job shell
defaults and local composite actions. New configurations and eligible legacy
migrations declare the `ruff` Wasm plugin; normal checking loads its exact pinned
artifact. See [plugin installation](plugins.md#install-from-the-registry).
`--no-tool ruff` disables Ruff. A bare `--tool ruff`
explicitly selects `ruff` on `PATH`, `--tool ruff=/path/to/ruff` selects a binary, and
`--tool ruff='python -m ruff'` uses a Python module launcher. Without a selected plugin
or explicit backend, the CLI searches `PATH` and skips Ruff
when unavailable. It never executes the Python scripts being checked.

```yaml
tools:
  ruff:
    enabled: true
    config:
      target-version: py314
      lint:
        extend-select: [B, S]
        ignore: []
lint:
  rules:
    external:
      ruff: error
```

Ruff validates `config` using its native options, including nested `lint` settings and rule-specific options. Omission or null selects the engine's default for an option.

The legacy flat `target-version`, `select` and `ignore` keys are shorthands for
`config.target-version`, `config.lint.select` and `config.lint.ignore`. `select` and `ignore`
accept Ruff codes or prefixes. The selected
engine validates them, including preview selectors when `config.preview` is enabled.
Lists replace inherited lists. Flat `select` and `ignore` replace the corresponding lists in
`config.lint`. For Python targets, `config.target-version` takes precedence over the flat setting.
Ruff's own top-level `config.select` and `config.ignore` stay native Ruff options.
`select: []` selects no lint rules (Python syntax errors can still be reported).
If no target is configured, actionlint infers it from a versioned interpreter such as
`python3.9`. The selected engine checks whether it supports that version. Without a
versioned interpreter, the engine chooses its default. The native process adapter uses `py314`.

Following [Astral's actionlint integration](https://github.com/astral-sh/actionlint/blob/0141e7822652fbee3a1b878c3d83d9c1dce87d67/rule_ruff.go),
Ruff runs in isolated, read-only mode: no repository Ruff configuration, cache,
automatic fixes, or `noqa` exemptions. Use actionlint rule levels, per-file
overrides, and YAML suppressions for exceptions. Ruff codes such
as `F821` appear in structured output. Literal/plain scripts use the shared
YAML source map; unsupported scalar mappings fall back to the `run:` declaration.
Inline YAML suppressions target the diagnostic's reported line. A directive on
the YAML block header covers the script body.
Ruff skips a script when an unquoted Actions expression forms part of a Python
token, such as `item_${{ matrix.os }}` or `${{ inputs.major }}.0`, or supplies a
name in statements such as `import ${{ inputs.module }}`, or supplies a whole
statement that may define names used later in the script. Other steps and
actionlint's workflow checks still run. Templates inside strings and comments
continue to be sanitized while preserving source positions.
Explicitly requested Ruff executables and Ruff invocation/configuration failures fail analysis.

The Go library opts in with `analysis.Options.Tools["ruff"]`, selecting either a
`Process` or an `analyzer.ScriptBackend`; a zero `analysis.Tool` disables it.
See [the library API](api.md#script-engine-backends) for cancellation, configuration
and backend lifetime requirements.
The GitHub Action runs Ruff unless its `disable-tools` input lists `ruff`.
It uses the configured Ruff plugin; the Action does not install native Ruff
or add it to `PATH`. The default `plugin-cache: true` restores and saves the
selected Wasm artifact bytes, keyed by operating system and exact digests.
Restored bytes are verified before execution. See [Action caching](plugins.md#github-action-caching).
The browser playground cannot run external tools.

## Tool execution backends

Each `tools` key is the rule name of a tool provider: the bundled `ruff` and
`shellcheck`, or a tool provider a plugin registers. Other names, including those of
plugin document analyzers, are rejected. Every
entry accepts `enabled`, `backend` and `config`, and `true` or `false` as a shorthand
for `enabled`. A bundled provider's schema validates inline `config` settings; a
string `config` selects a configuration file and is accepted only by tools that
read one, such as ShellCheck.

The published schema knows only the bundled providers, so it accepts any mapping as
the `config` of a provider a plugin installs. `actionlint config schema` prints the
schema with a `tools.<name>` entry for each tool provider manifest that `--plugin`
or the configuration's `plugins` installs, validated by the manifest's
`config_schema`. Save it and point the editor directive at the copy. It reads the
manifests without running a plugin or downloading an executable.

Engine configuration and execution are separate. `tools.<name>.config` configures
checks; `tools.<name>.backend` selects how the engine runs. `process` invokes a native
executable and `wasm` loads a typed script-engine component or a legacy core engine
in-process. Prefer root `plugins` declarations for registry-managed dependencies;
explicit backends are useful for a native tool or a directly selected script
engine. Library callers may also supply an engine backend without an executable.

```yaml
tools:
  shellcheck:
    backend:
      process:
        executable: mise
        args: [exec, --silent, --quiet, "github:kjanat/shellcheck-rs@latest", --, rshellcheck]
        env: { SHELLCHECK_OPTS: "" }
    config:
      enable: all # true and [all] have the same effect
  ruff:
    backend:
      wasm:
        path: ./tools/plugin.component.wasm.gz
    config:
      lint: { extend-select: [B, S] }
```

`tools.shellcheck.config.enable` selects optional checks: use `all`, `true`, or `[all]`
for every optional check, or a list of check names. Use `[]` to clear inherited
optional checks. `tools.shellcheck.enabled` controls whether the tool runs.

Executable paths are literal. Relative paths resolve from the declaring config
file, including inherited files and file overrides; inline configs use the analysis
working directory. Bare names use `PATH`. Arguments are literal launcher arguments
prepended to the engine invocation; they are never shell source. Environment entries
affect the child only. Explicit CLI, environment or library invocation settings take
precedence over repository process settings.

Repository-selected native executables require `--allow-config-processes`. Enable
this flag only for trusted configuration, including its inherited files and file
overrides. Without host opt-in, actionlint reports the selected process backend as
an operational error before launching it. Explicit CLI, environment and library
process selections already authorize their own executable.

A `wasm` backend accepts a component implementing the typed `script-engine`
interface or a core module implementing the version 1 JSON ABI. A document-only
analyzer, such as the official ShellCheck component, belongs in root `plugins`,
not `tools.shellcheck.backend.wasm`. Artifacts can be raw or gzip-compressed.
Local paths resolve from the declaring configuration file; remote execution
requires a `#sha256=` pin covering the downloaded bytes, including compression,
or an `oci://<host>/<repository>@sha256:<hex>` reference to an image with an
`application/wasm` layer.

actionlint validates exports and metadata before registering the engine. Component
calls default to 64 MiB of guest memory and a 10-second limit; core calls default
to 256 MiB and 10 seconds. Traps and exceeded limits fail that document's analysis.
Core engines cache compiled code under the user cache directory in
`actionlint/wazero`; downloaded artifacts use the separate verified artifact cache.
An explicit `--tool ruff` command, including `--tool ruff=false`, takes precedence over a
configured `tools.ruff.backend.wasm`. Selecting a Ruff plugin and an explicit
competing command/backend is rejected; select one execution source.
See [script engines](plugins.md#script-engines) for lifecycle and Go APIs.

The pinning rule supports scoped exceptions without suppressing other action checks:

```yaml
lint:
  rules:
    policy:
      require-commit-hash:
        level: true
        options:
          exclude: ["my-org/*"]
```

Patterns without `@` match the repository or complete action path; repository
matches also cover sub-actions and reusable workflows. Patterns containing `@`
match the complete reference including its ref. Include `docker://` when matching container references.

## Additional correctness checks

`correctness.unsound-ternary` reports `a && b || c` when the shared expression
evaluator can determine that `b` is falsy, including literal `fromJSON()` calls.
Unknown middle operands are left alone. The check applies to expressions in
workflow values, including `if:` conditions.

`correctness.workflow-run-names` checks `workflow_run.workflows` against the names
of workflows directly inside the repository's `.github/workflows` directory.
It accepts GitHub's workflow-name filter patterns and escaped special characters.
Negative patterns exclude names and do not require a matching producer.
Unnamed workflows use their repository-relative file path. Name resolution also
reads workflows excluded from lint inputs. The
check needs repository context and skips name matching if a workflow cannot be
read or parsed reliably. It does not query GitHub or inspect other branches.

Both rules are enabled by default and support rule levels, overrides, and inline
suppression like other registered rules.

## ShellCheck

Use `actionlint --log-level debug` to inspect each script's selected dialect,
working directory, rc arguments, inline directives and effective command options.
The log explains when an unresolved working directory disables source following.

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

`tools: {shellcheck: false}` disables ShellCheck; `true` enables it. The default is
enabled for the selected plugin or available native executable. New configurations
and eligible legacy migrations select the official ShellCheck Wasm plugin. This
boolean alone does not install a dependency or override `--no-tool shellcheck`
or the Action's `disable-tools: shellcheck` input.

The official registry plugin accepts individual disabled codes and optional check
names through these settings. Explicit rc files, disabled-code ranges or `all`
require a native ShellCheck executable. Source access and
search paths work through the component's read-only host grants described below.
To select a native process explicitly, use `tools.shellcheck.backend.process` or
`--tool shellcheck=/path/to/shellcheck`.

`config` accepts the inline settings below, an rc-file path, or a directory in
which `.shellcheckrc`, then `shellcheckrc`, is searched. Rc discovery is otherwise
disabled by default. Explicit selections must exist and be readable.

```yaml
tools:
  shellcheck:
    config: ./.shellcheckrc # Relative to actionlint.yaml, not the workflow.
```

| Inline key          | Accepted values                                          |
| ------------------- | -------------------------------------------------------- |
| `disable`           | List of codes, ranges such as `SC3000-SC4000`, or `all`. |
| `enable`            | List of optional check names, or `all`.                  |
| `shell`             | `sh`, `bash`, `dash`, `ksh`, or `busybox`.               |
| `extended-analysis` | Boolean; omission keeps ShellCheck's default.            |
| `external-sources`  | Boolean; defaults to enabled in actionlint.              |
| `source-path`       | List of directories searched for sourced files.          |

`enable` selects optional checks; it does not override actionlint's
[built-in diagnostic exclusions](checks.md#shellcheck-integration-for-run). For example,
`check-unassigned-uppercase` produces SC2154, which actionlint suppresses because
workflow environment variables can be supplied externally. This applies equally
to inline settings, rc files, script directives and command-line options.

Rc paths supplied by an inline configuration overlay use the analysis working
directory (the Action's `working-directory`). Inherited paths keep their config
file's directory; explicit `${{ configdir }}` always selects that directory.

Config paths and inline `source-path` entries accept these substitutions:

| Expression                  | Meaning                                                                                                                                                      |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `${{ configdir }}`          | Directory containing the selected actionlint configuration; also the base for relative rc paths. Falls back to the repository root, then analysis directory. |
| `${{ gitdir }}`             | Analyzed repository root; falls back to the analysis directory when no project is found.                                                                     |
| `${{ github.workspace }}`   | `GITHUB_WORKSPACE`, or the local repository root.                                                                                                            |
| `${{ github.action_path }}` | Analyzed composite action's directory; unavailable for ordinary workflow steps.                                                                              |

An unrelated `GITHUB_ACTION_PATH` does not replace the analyzed action's directory.
Unknown variables and unavailable contexts are configuration errors. Substitution
happens once. In workflow inputs, GitHub evaluates expressions first; pass a literal
with `${{ '${{ configdir }}/.shellcheckrc' }}` when needed.

The Action's `config` input overlays these settings: lists replace, boolean shorthand
changes only `enabled`, and `null` restores defaults. Configure ShellCheck through
`tools.shellcheck.config`; the Action forwards those options to the configured plugin.

Inline settings use a separate [ShellCheck 0.11.0 schema][shellcheck-schema].
That schema version does not pin the installed executable; optional checks depend
on its version. See the [ShellCheck manual][shellcheck-manual] for native settings.

### Script selection and directives

ShellCheck checks recognized shell steps, including custom templates such as
`bash -euxo pipefail {0}`, interpreter paths and `.exe` names. Supported templates
include `bash`, `sh`, `dash` and `ksh`. Unknown wrappers and templates using `-c`
or `-s` need a leading directive to identify the embedded script:

```yaml
- shell: custom-shell {0}
  run: |
    # shellcheck shell=bash
    echo "$HOME"
```

The directive selects the analysis dialect. GitHub's runtime interpreter stays
unchanged. Other workflow checks still run when ShellCheck skips an unknown
language. A global dialect override does not select Python, PowerShell or unknown
wrappers for ShellCheck.

Dialect precedence, highest first:

1. ShellCheck command arguments, then `SHELLCHECK_OPTS`.
2. A leading `# shellcheck shell=...` directive.
3. Application settings, then inline `config.shell`.
4. The workflow's resolved shell, ahead of any shebang.

Diagnostics from native [directives][shellcheck-directives] map to YAML positions.
Template options before `{0}` contribute startup settings;
later options override earlier ones, while script arguments after `{0}` do not.
Unknown options leave startup assumptions unset. Changing the inferred dialect
discards those assumptions. Final option states are normalized because ShellCheck
0.11.0 can treat an earlier `set -e` or `set -o pipefail` as active after it is disabled.
The user's script is not rewritten. Debug logs explain analyzer selection.

### Source resolution

Sourced files and relative `source-path` entries resolve from the effective run
directory: step `working-directory`, job default, workflow default, then workspace
root. For `working-directory: app` and `source-path: [scripts]`, the search path is
`app/scripts`. The ShellCheck process runs there too; custom wrapper arguments stay
literal, so use absolute paths for wrapper-owned files elsewhere.

For a native process, available parent directories, symlink targets and absolute paths with compatible
host/runner syntax can be analyzed, including outside the repository. Dynamic,
missing or unrepresentable working directories limit analysis to the embedded
script. `SCRIPTDIR` does not mean the YAML
directory: embedded scripts reach ShellCheck through stdin. Paths refer to
actionlint's local filesystem; job containers and their mounts are not recreated.

The official Wasm plugin follows sources when `external-sources: true` or an explicit
`source-path` enables access. The host grants the workspace and configured search
directories read-only; workflow directives cannot add grants. An outside working
directory must fall within an explicitly configured search directory. Absolute
source directives inside those grants are translated to the guest filesystem,
including Windows paths. Missing search directories are ignored and recorded as
dependencies. Symlinks cannot escape a granted root. Files are snapshotted on first
read; subsequent reads and sourced diagnostic snippets use the same bytes.

All successful Wasm source reads, including clean and transitively sourced files,
enter analysis inputs. Metadata lookups also enter the dependency list, so creating
an earlier search candidate changes the observed inputs. One invocation allows
8 MiB of source bytes, 256 source files, 4096 lookups and 32 configured search
directories; exceeding a limit makes analysis incomplete even if the guest ignores
the read error. Source snapshots come from disk; unsaved source-file buffers are
not substituted. Reanalysis obtains new snapshots after source edits.

Native shellcheck-rs reports its complete source inventory in `json1.sources`;
actionlint records it independently of diagnostics, including sources outside the
workspace. Haskell ShellCheck does not expose this field: its dependency list can
only include files named by diagnostics and is not exhaustive. Both recorded
dependencies and Wasm reads protect those paths from being overwritten by report
output.

Referenced local composite actions, including nested ones, are checked too. Their
steps use their own shell and working directory, without inheriting workflow/job
defaults. Relative composite working directories start at the workspace root;
`${{ github.action_path }}` selects the action's directory.

## Policy checks

The three cache checks below default to enabled. Cache safety checks belong to
`lint.rules.security`; `cache-operation` belongs to `lint.rules.correctness`.
Other policies are opt-in under `lint.rules.policy`. Omitted rules and `level: default`
keep defaults; `level: off` explicitly disables a rule, including under `--strict`.
Policy names also identify their diagnostics.

### cache-call-unrestricted

Requires an explicit cache ceiling at reusable calls on low-trust triggers, set on
the job or inherited from the workflow. Use `cache-mode: read` or `none` to cap
the callee. A write-capable declaration satisfies this check but may trigger
`cache-write-untrusted`. Applies to local and remote calls without downloading
remote workflow bodies. Disable with
`lint: {rules: {security: {cache-call-unrestricted: off}}}`.

### cache-operation

Reports official cache actions disabled by the effective explicit cache mode:

| Action                  | Modes reported       |
| ----------------------- | -------------------- |
| `actions/cache/save`    | `read`, `none`       |
| `actions/cache/restore` | `write-only`, `none` |
| `actions/cache`         | `none`               |

Follows inherited ceilings through local reusable workflows. Inherited violations
are reported at the caller's `uses`; explicit callee modes are checked in the
callee. Omitted modes, custom wrappers and remote bodies are not guessed.
Disable with `lint: {rules: {correctness: {cache-operation: off}}}`.

### cache-write-untrusted

Reports explicit `write` or `write-only` cache grants on low-trust triggers using
default-branch caches. Only declarations are checked; `if` guards and cache
contents are ignored.
Use `read` or `none`, or document a reviewed [inline exception](#inline-diagnostic-suppressions).
Ordinary `pull_request`, review events, `merge_group`, trusted write-default events
such as `push`, and standalone `workflow_call` do not trigger this policy.
Checked triggers: `branch_protection_rule`, `check_run`, `check_suite`, `deployment`,
`deployment_status`, `discussion`, `discussion_comment`, `fork`, `gollum`,
`image_version`, `issue_comment`, `issues`, `label`, `milestone`, `public`,
`pull_request_target`, `status`, `watch` and `workflow_run`.
See [cache checks](checks.md#cache-safety-policies) for diagnostic examples.
Disable with `lint: {rules: {security: {cache-write-untrusted: off}}}`.

### Local action source mappings

When a workflow generates an action directory, map its repository-relative path to the
checked-in source directory:

```yaml
local-actions:
  ./generated-action: ./actions/source
```

Actionlint reads `actions/source/action.yml` or `action.yaml` to check the action's
metadata, inputs and outputs. Both paths are relative to the repository root;
known checkout paths are accounted for when resolving `uses`. Each mapping resolves
once to an actual source directory. Per-file overrides can provide different
`local-actions` mappings for different workflows.
Missing action sources and invalid source entrypoints are errors.

Local action lookup checks `action.yml`, `action.yaml`, `Dockerfile`, then `dockerfile`, in that order.
This also applies to source mappings. A Dockerfile-only action is executable by GitHub's Linux runner but supplies
no manifest schema: actionlint leaves its input and output names unknown. `with.args` and `with.entrypoint`
are accepted, as are other undeclared inputs; actionlint cannot infer their meaning from a Dockerfile.
The Dockerfile is recorded as an analysis input for dependency tracking and output-collision checks.
It is not parsed as a YAML action manifest. See the [runner's fallback implementation](https://github.com/actions/runner/blob/5f0b3ddcc6ee9fe9441e00d55e85b94ea4aad798/src/Runner.Worker/ActionManager.cs#L875-L912).

### Reusable ignores

Declare repeated exceptions once in configuration:

```yaml
ignores:
  - rule: missing-local-action
    uses: ./generated-action
    file: .github/workflows/ci.yml
    reason: Release bundle built during CI.
```

An entry selects a rule and at least one of `file`, `uses`, `job` or `step`.
All supplied selectors must match. `file` accepts repository-relative doublestar
globs against the reported file, including a sourced script for ShellCheck findings.
`uses`, `job` and `step` select the invocation that produced the finding and match exact values, with `step` accepting its ID or
name. `reason` is optional. This example suppresses
only missing-manifest findings for the selected action in `ci.yml`; other action
diagnostics still apply.

Inside a composite action, `job` matches the calling workflow job, including through
nested composites. `step` and `uses` match the composite's own step, and `file`
still matches the reported file. Ignoring one job does not hide findings from
another job that calls the same action.

With `extends`, an omitted `ignores` keeps inherited entries. A supplied list
replaces them; `ignores: []` clears them. The `disallow-suppressions` policy also
applies to these entries. An ignore targeting `disallow-suppressions` is rejected.

### Inline diagnostic suppressions

```yaml
cache-mode: write # actionlint:ignore cache-write-untrusted -- reviewed default-branch code only
```

Or place `# actionlint:ignore-next-line RULE -- reason` immediately before the
reported line. The ` -- reason` suffix is optional. Comma-separated selectors accept
any diagnostic rule ID listed by `actionlint rules`, including `expression`,
`syntax-check`, `shellcheck`, `if-cond` and the suspicious rules.
Exceptions apply to the reported physical line. A directive on a YAML block scalar
header (`run: |` or `run: >`) covers its body as well. A preceding
`actionlint:ignore-next-line` directive can also target that header. Blank lines or
other comments detach a preceding directive. For aliases, use the reported anchor
location.

For ShellCheck findings in sourced scripts, place a directive on
the originating `run` declaration or its YAML block header. This suppresses findings
from that invocation while keeping their reported script locations. Comments inside
sourced scripts do not create actionlint suppression directives.

```yaml
- run: | # actionlint:ignore expression,shellcheck -- reviewed script inputs
    echo '${{ github.event.issue.title }}'
```

Malformed attached directives report `inline-suppression`. Only attached YAML
comments create directives. Directives in local composite YAML are scoped to that
file. CLI, rule-ID and path ignores apply afterwards. Suppression
changes lint output. A check with unsuppressed findings exits with 1.

Suppressions apply to errors and warnings alike. To suppress a rule for a whole
file, use `lint.rules.disable` in an [override](#per-file-overrides). This also works
for malformed YAML where parsing cannot attach inline comments. Configuration,
I/O and tool-launch failures cannot be suppressed.

### disallow-suppressions

`lint.rules.policy.disallow-suppressions: on` prohibits inline and configured exceptions.
To select rules and output:

```yaml
lint:
  rules:
    policy:
      disallow-suppressions:
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

`on` without options selects all supported diagnostic rules; explicit `options.rules` must be nonempty.
Omission or `off` permits exceptions. The policy cannot exempt itself,
enable a disabled rule or override CLI/path ignores. `all` and `suppression` report
prohibited inline directives even when no underlying finding exists. Configured
exceptions are reported when they match a finding.

### require-commit-hash

`lint.rules.policy.require-commit-hash: on` requires 40- or 64-digit hexadecimal refs for
actions/reusable workflows and digests for `docker://` images. Local `./` and `$/`
references and unresolved expressions are skipped.

### require-job-timeout

`lint.rules.policy.require-job-timeout: on` requires `timeout-minutes` on jobs running
steps. Reusable-call jobs are excluded. Optional inclusive bounds:

```yaml
lint:
  rules:
    policy:
      require-job-timeout:
        level: on
        options: { min-minutes: 5, max-minutes: 60 }
```

Either bound may be omitted; both must be finite and positive, with minimum no
greater than maximum. Omitted options require the key only. Expression values are not compared.

### require-permissions

`lint.rules.policy.require-permissions: on` requires workflow-level `permissions`.
Use `{level: on, options: {scope: job}}` to require it on every job,
including reusable calls. Empty mappings, named scopes, `read-all` and `write-all`
all satisfy the check. This policy only requires a declaration; it does not assess
least privilege.

### required-actions

```yaml
lint:
  rules:
    policy:
      required-actions:
        level: on
        options: { actions: [actions/checkout, "my-org/security-scan@v2*"] }
```

Patterns match action names case-insensitively and refs case-sensitively; `*` does
not cross `/`. Omitting `@ref` accepts any ref. Search is limited to steps declared
directly in the workflow. Workflows containing unresolved `uses`
expressions or only reusable-call jobs are skipped.

## Generate the initial configuration

```sh
actionlint -init-config
```

This writes `.github/actionlint.yaml` with the editor schema directive.

---

[Checks](checks.md) | [Installation](install.md) | [Usage](usage.md) | [Go API](api.md) | [References](reference.md)

[pat]: https://pkg.go.dev/path#Match
[doublestar]: https://github.com/bmatcuk/doublestar
[shellcheck-directives]: https://www.shellcheck.net/wiki/Directive
[shellcheck-schema]: ../schemas/shellcheck/0.11.0.schema.json
[shellcheck-manual]: https://github.com/koalaman/shellcheck/blob/v0.11.0/shellcheck.1.md
