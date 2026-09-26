# Configuration

Configuration is optional. Put `actionlint.yaml` or `actionlint.yml` in `.github/`,
or select a file with `actionlint --config path/to/actionlint.yaml`.
The CLI and GitHub Action use the same settings.

## Configuration file

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/kjanat/actionlint/HEAD/actionlint.schema.json
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
The schema rejects unknown keys. Runtime parsing still ignores unknown keys at the
top level, in `self-hosted-runner`, and in `paths` entries. Regex and glob validity
is checked when actionlint loads the file.

## ShellCheck

```yaml
tools:
  shellcheck:
    enabled: true
    config:
      disable: [SC2086]
      enable: [check-unassigned-uppercase]
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
Use `read` or `none`, or document a reviewed [inline exception](#inline-cache-policy-exceptions).
Ordinary `pull_request`, review events, `merge_group`, trusted write-default events
such as `push`, and standalone `workflow_call` do not trigger this policy.
Checked triggers: `branch_protection_rule`, `check_run`, `check_suite`, `deployment`,
`deployment_status`, `discussion`, `discussion_comment`, `fork`, `gollum`,
`image_version`, `issue_comment`, `issues`, `label`, `milestone`, `public`,
`pull_request_target`, `status`, `watch` and `workflow_run`.
See [cache checks](checks.md#cache-safety-policies) for diagnostic examples.
Disable with `policy: {cache-write-untrusted: false}`.

### Inline cache policy exceptions

```yaml
cache-mode: write # actionlint:ignore cache-write-untrusted -- reviewed default-branch code only
```

Or place `# actionlint:ignore-next-line RULE -- reason` immediately before the
reported line. A nonempty reason is required. Comma-separated selectors can name
`cache-call-unrestricted`, `cache-operation` and `cache-write-untrusted` only.
Exceptions apply only to that physical line. Blank lines or other
comments detach a preceding directive. For multiline values, use the diagnostic's
line; for aliases, use the reported anchor location.

Malformed attached directives report `inline-suppression`. Text inside strings or
scripts is not a directive. CLI and path ignores apply afterwards. Suppression
changes lint output. GitHub's cache permissions stay unchanged; remaining findings
exit with 1.

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

`true` or `{}` selects all supported cache rules; explicit `rules` must be nonempty.
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
