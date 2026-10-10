# Configuration

Configuration is optional. Put `actionlint.yaml` or `actionlint.yml` in `.github/`,
or select a file with `actionlint --config path/to/actionlint.yaml`.
The CLI and GitHub Action use the same settings.

## Configuration file

```yaml
# yaml-language-server: $schema=../node_modules/@kjanat/actionlint/actionlint.schema.json
self-hosted-runner:
  labels: [linux.2xlarge, windows-latest-xl]
config-variables: [DEFAULT_RUNNER, ENVIRONMENT_STAGE]
config-secrets: [DEPLOY_TOKEN]
assume-default-permissions: restricted
paths:
  .github/workflows/**/*.{yml,yaml}:
    ignore:
      - "shellcheck reported issue in this script: SC2086:.+"
```

| Key                          | Purpose                                                                                                                                                         |
| ---------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `self-hosted-runner.labels`  | Additional runner labels; supports [`path.Match` patterns][pat].                                                                                                |
| `config-variables`           | Allowed `vars` names. Omitted or `null` disables the check; `[]` allows none.                                                                                   |
| `config-secrets`             | Allowed secret names, case-insensitive. Omitted or `null` disables the check; `[]` allows only built-in and declared `workflow_call` secrets.                   |
| `assume-default-permissions` | Assumed repository token default for reusable calls without explicit permissions: `restricted` (default) or `permissive`. Neither implicitly grants `id-token`. |
| `paths`                      | Repository-relative [glob patterns][doublestar], using `/`. Each `ignore` list contains regular expressions matched against diagnostic messages.                |

The [schema](../actionlint.schema.json) provides editor completion and validation.
An installed npm package also supplies it at
`../node_modules/@kjanat/actionlint/actionlint.schema.json` from a `.github/` config;
see [schema distribution](../distribution/npm/facade/README.md#configuration-schema).
Without an installed package, select a matching published version from that guide.
Use a source-commit schema when trying unreleased settings; `HEAD` changes with development.
The full editor experience works in a real config file. YAML inside the Action
`config` string is validated at runtime; editors generally see only a string.
The schema rejects unknown keys. Runtime parsing warns about ignored keys at the
top level, in `self-hosted-runner`, and in `paths` entries, with their locations
and accepted alternatives. New inline overlays reject unknown keys. Regex and glob
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
rc paths retain the directory of their declaring configuration.

Only local files are supported: no remote downloads, package resolution, or
commands. Missing bases, cycles, invalid base settings, and chains deeper than
32 levels are errors. `ReadConfigFile` and file-based CLI configuration resolve
inheritance. In-memory `ParseConfig` and inline Action overlays cannot resolve
`extends`; select a configuration file instead.

## File selection

Top-level `files` controls which input files are analyzed, independently of lint
rule settings. Patterns are repository-relative doublestar globs using `/`; when
there is no repository, they are relative to the working directory. They filter
selected inputs and do not discover additional files.

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
Each entry can overlay `lint` and `tools`. Tool settings merge independently of
rule settings, retaining inherited options that the override leaves unspecified.
External tool provisioning uses each selected file's effective settings.

## Lint rules

The `lint` section owns analysis settings. Rules are grouped by concern:
`correctness`, `suspicious`, `security`, `policy`, and `external`. The separate
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

Every catalog rule accepts `off`, `on` (retain its native severity), `info`, `warn`,
or `error`. Use `default` to reset a rule or group override to the applicable
preset/group baseline. Empty strings are invalid. The object form requires `level` and accepts typed `options` only where
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
suppresses all lint diagnostics. Configuration, I/O and other operational errors remain fatal.

Resolution order is: existing defaults/legacy policy settings, global preset,
CLI presets, group preset/level, individual rule settings, then explicit exclusion
lists. File overrides are merged before that resolution. `--experimental=false`
is an explicit exclusion of nursery rules only. `--strict` selects all stable
rules but respects group and individual exceptions. Existing top-level `policy`
settings remain supported; `lint.rules` settings take precedence. The cache safety
checks are classified under security (and cache-operation under correctness);
their legacy configuration paths remain compatible.

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
filter entries and wholly dynamic matrices remain outside that check's scope.
See [expression behavior](expression-behavior.md) for evidence and limitations.

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
Group and rule level shorthands retain their level when overlaid by a partial
mapping. Rule object settings require `level`; omitted option keys inherit.
Each file gets its own effective configuration, without changing other files.

## Ruff

Ruff checks Python scripts selected by `shell: python`, `shell: python3`, or a
custom command beginning with either interpreter, including workflow/job shell
defaults and local composite actions. The CLI discovers Ruff on `PATH` and skips
it when unavailable. `--ruff=false` or `--ruff=` disables it;
`--ruff /path/to/ruff` selects a binary.
`--ruff 'python -m ruff'` runs Ruff through a Python module launcher.
It never installs tools or executes Python scripts.

```yaml
tools:
  ruff:
    enabled: true
    target-version: py314
    select: [F]
    ignore: []
lint:
  rules:
    external:
      ruff: error
```

The default selection is Ruff's `F` correctness checks. `select` and `ignore`
accept Ruff codes or prefixes matching stable rules; preview-only selectors are
rejected. Lists replace inherited lists. `select: []` selects
no lint rules (Python syntax errors can still be reported). `target-version`
defaults to the version in an explicit interpreter such as `python3.9`, when
supported by Ruff, and otherwise to `py314`. An explicit setting takes precedence.

Following [Astral's actionlint integration](https://github.com/astral-sh/actionlint/blob/0141e7822652fbee3a1b878c3d83d9c1dce87d67/rule_ruff.go),
Ruff runs in isolated, read-only mode: no repository Ruff configuration, cache,
automatic fixes, or `noqa` exemptions. Use actionlint rule levels, per-file
overrides, and reason-bearing YAML suppressions for exceptions. Ruff codes such
as `F821` are retained in structured output. Literal/plain scripts use the shared
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

The Go APIs opt in with `AnalysisOptions.Ruff`, `AnalysisRequest.Ruff`, or
`LinterOptions.Ruff`; an empty command disables the tool. The analysis APIs also
accept `RuffOptions` for literal executable paths, arguments, and child environment.
The GitHub Action's `ruff` input defaults to `true`.
The Action installs Ruff when enabled and required by the effective configuration.
It reuses a binary on `PATH`, then its cache, then a checksum-verified release download.
The browser playground cannot run external tools.

## Additional correctness checks

`correctness.unsound-ternary` reports `a && b || c` when the shared expression
evaluator can determine that `b` is falsy, including literal `fromJSON()` calls.
Unknown middle operands are left alone. The check applies to expressions in
workflow values, including `if:` conditions.

`correctness.workflow-run-names` checks `workflow_run.workflows` against the names
of workflows directly inside the repository's `.github/workflows` directory.
It accepts GitHub's workflow-name filter patterns and escaped special characters.
Negative patterns exclude names and do not require a matching producer.
Unnamed workflows use their repository-relative file path. Producer workflows
remain available for name resolution even when excluded from lint inputs. The
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
enabled when the executable is available. This setting does not install it or
override an empty CLI ShellCheck command or the Action's `shellcheck: false` input.

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
changes only `enabled`, and `null` restores defaults. `shellcheck-args` uses native
ShellCheck precedence. The separate `shellcheck-rc` input accepts `true`, `false`
or an absolute/Action-working-directory-relative rc path, without interpolation. It overrides rc
selection; `false` leaves inline settings active.

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

Native [directives][shellcheck-directives] retain their scope, and diagnostics map
back to YAML positions. Template options before `{0}` contribute startup settings;
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

Available parent directories, symlink targets and absolute paths with compatible
host/runner syntax can be analyzed, including outside the repository. Dynamic,
missing or unrepresentable working directories disable source following while
retaining analysis of the embedded script. `SCRIPTDIR` does not mean the YAML
directory: embedded scripts reach ShellCheck through stdin. Paths refer to
actionlint's local filesystem; job containers and their mounts are not recreated.

Referenced local composite actions, including nested ones, are checked too. Their
steps use their own shell and working directory, without inheriting workflow/job
defaults. Relative composite working directories start at the workspace root;
`${{ github.action_path }}` selects the action's directory.

## Policy checks

The three cache policies below default to enabled. Other policies are opt-in.
Omitted or `null` values keep defaults; `false` disables a boolean policy and `[]`
disables `required-actions`. `policy: {}` does not disable cache checks.
Policy names also identify their diagnostics.

### cache-call-unrestricted

Requires an explicit cache ceiling at reusable calls on low-trust triggers, set on
the job or inherited from the workflow. Use `cache-mode: read` or `none` to cap
the callee. A write-capable declaration satisfies this check but may trigger
`cache-write-untrusted`. Applies to local and remote calls without downloading
remote workflow bodies. Disable with `policy: {cache-call-unrestricted: false}`.

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
Disable with `policy: {cache-operation: false}`.

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
Disable with `policy: {cache-write-untrusted: false}`.

### Inline diagnostic suppressions

```yaml
cache-mode: write # actionlint:ignore cache-write-untrusted -- reviewed default-branch code only
```

Or place `# actionlint:ignore-next-line RULE -- reason` immediately before the
reported line. A nonempty reason is required. Comma-separated selectors accept
any diagnostic rule ID listed by `actionlint rules`, including `expression`,
`syntax-check`, `shellcheck`, `if-cond` and the suspicious rules.
Exceptions apply to the reported physical line. A directive on a YAML block scalar
header (`run: |` or `run: >`) covers its body as well. A preceding
`actionlint:ignore-next-line` directive can also target that header. Blank lines or
other comments detach a preceding directive. For aliases, use the reported anchor
location.

For ShellCheck findings in sourced scripts, place a reason-bearing directive on
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
changes lint output. GitHub's cache permissions stay unchanged; remaining findings
exit with 1.

Suppressions apply to errors and warnings alike. To suppress a rule for a whole
file, use `lint.rules.disable` in an [override](#per-file-overrides). This also works
for malformed YAML where parsing cannot attach inline comments. Configuration,
I/O and tool-launch failures remain operational errors and cannot be suppressed.

### disallow-suppressions

`policy: {disallow-suppressions: true}` prohibits all supported inline exceptions.
To select rules and output:

```yaml
policy:
  disallow-suppressions:
    rules: [cache-call-unrestricted, cache-write-untrusted]
    report: all
```

| `report`        | Prohibited directive | Original finding |
| --------------- | -------------------- | ---------------- |
| `all` (default) | Reported             | Retained         |
| `suppression`   | Reported             | Suppressed       |
| `violation`     | Not reported         | Retained         |

`true` or `{}` selects all supported diagnostic rules; explicit `rules` must be nonempty.
Omission, `null` or `false` permits exceptions. The policy cannot exempt itself,
enable a disabled rule or override CLI/path ignores. `all` and `suppression` report
prohibited directives even when no underlying finding exists.

### require-commit-hash

`policy: {require-commit-hash: true}` requires 40- or 64-digit hexadecimal refs for
actions/reusable workflows and digests for `docker://` images. Local `./` and `$/`
references and unresolved expressions are skipped.

### require-job-timeout

`policy: {require-job-timeout: true}` requires `timeout-minutes` on jobs running
steps. Reusable-call jobs are excluded. Optional inclusive bounds:

```yaml
policy:
  require-job-timeout: { min-minutes: 5, max-minutes: 60 }
```

Either bound may be omitted; both must be finite and positive, with minimum no
greater than maximum. `{}` requires the key only. Expression values are not compared.

### require-permissions

`policy: {require-permissions: true}` requires workflow-level `permissions`, as do
`{}` and `{scope: workflow}`. Use `{scope: job}` to require it on every job,
including reusable calls. Empty mappings, named scopes, `read-all` and `write-all`
all satisfy the check. This policy only requires a declaration; it does not assess
least privilege.

### required-actions

```yaml
policy:
  required-actions: [actions/checkout, my-org/security-scan@v2*]
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
