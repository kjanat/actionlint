---
title: actionlint
section: 1
header: General Commands Manual
footer: actionlint 1.16.1
---

# NAME

**actionlint** - static checker for GitHub Actions workflow files

# SYNOPSIS

**actionlint** \[*flags*\]\
**actionlint** \[*flags*\] *file*...\
**actionlint** \[*flags*\] -

# DESCRIPTION

**actionlint** checks GitHub Actions workflow files without executing the workflows. This is
the maintained `kjanat/actionlint` fork, distributed as the Go module `actionlint.kjanat.dev`.

Checks include workflow syntax, expression types and context availability, action inputs and
outputs, local action metadata and composite steps, reusable workflow inputs and permissions,
job dependencies, parallel steps, runner labels, event filters, schedules, and YAML anchors.
Security checks report potentially unsafe expression interpolation in scripts and hard-coded
credentials. Optional ShellCheck and pyflakes integrations check scripts in `run:` steps.

Repository policy checks can require immutable action references, job timeouts, and particular
actions. These checks are enabled explicitly in the configuration file; ordinary workflow
checks do not require configuration.

# USAGE

With no paths, **actionlint** and **actionlint check** find the current repository
containing both `.git` and `.github/workflows`, then recursively check workflow
YAML files in that directory. Explicit file arguments and a lone **-** for stdin
remain supported. Passing a directory does not enable recursive input expansion;
passing an action manifest does not select standalone action-metadata linting.

    $ actionlint
    $ actionlint file1.yaml file2.yaml
    $ actionlint check file1.yaml --output-format=json
    $ actionlint check --stdin-filename .github/workflows/ci.yml -

The root stops option parsing at the first filename. **check** accepts options
before or after filenames. **--** forces subsequent arguments to be filenames.
An existing regular file named like a command takes precedence on the root.
The hidden `--command NAME` escape explicitly selects a command in that case.

# COMMANDS

**check** [*flags*] [*files*...]
: Check workflows with the modern option grammar. Uses the same engine and
repository discovery as the root invocation.

**config init**
: Create `.github/actionlint.yaml` with its YAML Language Server directive.
Refuse to overwrite either supported config filename.

**config path**
: Print the selected configuration path without parsing it, or nothing if absent.

**config show** [**--origin**]
: Show effective configuration as YAML. **--json** returns `path` and `config`.
With `--origin`, include sources keyed by JSON Pointer: `default` or `config`,
missing/null/value state, and positions for file settings. Explicit false and
empty lists remain distinguishable from defaults and null.

**config validate**
: Validate with the same loader as analysis. This retains existing unknown-key
handling; it does not substitute the stricter editor JSON Schema validator.

**rules** [*name*]
: List checks by name and category, or explain one check. Supports **--json**.

**doctor**
: Inspect configuration and external-tool resolution without executing tools.
Missing optional tools are reported; malformed selected config returns status 3.

**completion** *shell*
: Print the generated completion script for the selected shell.

**version**
: Show version and build details. Supports **--json**.

# FLAGS

These options belong to the root and to **check**, unless noted otherwise.
Root parsing retains the original single- and double-dash spellings; **check**
uses long options and short aliases. Run **--help-legacy** for old spellings.

**--output-format**, **--output**, **-o** *MODE*
: Select `text`, `oneline`, `json`, `jsonl`, `sarif`, or `github`. The default is
`text`. **--output** is a supported alias. JSON emits a versioned document with
`schema_version` and `diagnostics`; JSONL emits one diagnostic per line. GitHub
annotations are emitted only when explicitly selected.

**--json**
: Select JSON output. With help, version, config, rules or doctor, emit JSON
metadata. Operational errors are JSON objects on stderr.

**--template**, **--format**, **-f** *TEMPLATE*
: Render diagnostics with a Go template. Existing template fields and functions
remain unchanged, including the JSON array returned by `{{json .}}`. Cannot be
combined with a built-in output selector. A nonempty template takes precedence
over the legacy **--oneline** option.

**--template-file** *PATH*
: Read a Go template from a file. Cannot be combined with an inline template.

**--output-file** *PATH*
: Write results to the given path. `-` means stdout. Replace the file
only after successful analysis, including analysis that found problems. A failure
preserves a previous report. The output cannot also be a selected input.

**--color**[=*MODE*]
: On **check**, select `auto`, `always`, or `never`; a bare flag means `always`.
On the root, retain the boolean syntax: `-color=false` does not force color on.
Help uses the same controls: automatic styling follows stderr's terminal status,
**NO_COLOR**, and **TERM=dumb**. Put root color flags before **--help**.
Structured output contains no terminal color codes.

**--no-color**
: Disable color. This supported legacy option wins over the root's **--color**
boolean regardless of argument order.

**--config**, **--config-file** *PATH*
: Select the configuration file. This takes precedence over repository discovery. **--config-file**
is the supported legacy name. Also available on **config** and **doctor**.

**--no-config**
: Use defaults without loading a configuration file. Cannot be combined with a
nonempty explicit config path. Also available on **config** and **doctor**.

**--ignore-regex**, **--ignore** *REGEXP*
: Suppress findings whose messages match this Go regular expression. Repeat to
add patterns. **--ignore** is the
supported legacy name.

**--stdin-filename** *PATH*
: Set the path used for stdin diagnostics and project detection. The default is
`<stdin>`. Use an existing workflow path to select its repository context.

**--log-level** *LEVEL*
: Select `none`, `info`, or `debug`. The default is `none`.

**--quiet**, **-q**
: Suppress progress, debug logs and summaries. Requested results and operational
errors remain visible. Also available on information and config commands.

**--summary**
: Print selected workflow and finding counts on stderr after checking. In JSON
modes this is a JSON record with a `summary` field. A normal clean run stays silent.

**--verbose**, **-v**
: Retain legacy progress logging. Prefer **--log-level**=info on **check**.

**--debug**
: Retain legacy debug logging. On the legacy route, **--verbose** wins when both
flags are true. An explicit **--log-level** selects the requested new log level.

**--oneline**
: Retain one-line diagnostic output. Prefer **--output-format**=oneline.

**--shellcheck** *COMMAND*
: ShellCheck command, executable path or command line with arguments. Defaults
to `shellcheck`; an empty value disables it. Also available on **doctor**.

**--pyflakes** *COMMAND*
: Pyflakes command, executable path or command line with arguments. Defaults to
`pyflakes`; an empty value disables it. Also available on **doctor**.

**--init-config**
: Root-only alias for **config init**, retaining existing initialization behavior.

**--completion**, **--completions** *SHELL*
: Root-only supported aliases for **completion** *SHELL*. Accept `bash`, `fish`,
`powershell`, `zsh`, `pwsh`, shell executable paths, and `auto`. Cobra generates
all four completion scripts. Install bash-completion for Bash and run compinit
for Zsh. Regenerate saved scripts after upgrading.

**--version**, **-V**
: Root-only version flag. Retains its original three-line output. Use the
**version** command for the new presentation or **version --json** for metadata.
Lowercase **-v** continues to enable verbose logging.

**--help**, **-h**
: Show grouped help on stderr, or JSON command metadata on stdout with **--json**.

**--help-legacy**
: Explain supported legacy option spellings and parsing rules without
introducing deprecation warnings.


# CONFIGURATION

Configuration is optional. In a detected repository, actionlint reads `.github/actionlint.yaml`,
or `.github/actionlint.yml` if the first filename is absent. **--config-file** selects a different
file; settings are not merged with the repository file or a user-global configuration.

The available settings are:

**self-hosted-runner.labels**
: Additional runner-label patterns. Patterns use Go `path.Match` glob syntax.

**config-variables**
: Allowed names in the `vars` context. Omitted or `null` disables this check; an empty list
allows no configuration variables.

**config-secrets**
: Allowed secret names, compared case-insensitively. Omitted or `null` disables this check.
The built-in secrets `GITHUB_TOKEN`, `ACTIONS_STEP_DEBUG`, and `ACTIONS_RUNNER_DEBUG`, and
secrets declared in `on.workflow_call.secrets`, remain allowed even with an empty list.

**assume-default-permissions**
: Permission assumption for a local reusable workflow call when neither the calling job nor
its workflow declares `permissions`. Defaults to `restricted`, which assumes read access to
`contents` and `packages`. `permissive` assumes write access; `id-token` still requires an
explicit grant. This setting does not change the repository's actual GitHub permissions.

**paths**
: Map repository-relative glob patterns to configuration. Patterns use `/` separators and
support `**` and brace alternatives. Each entry's `ignore` list contains message regular
expressions, applied in addition to **--ignore**.

**policy.require-commit-hash**
: When `true`, require action and reusable workflow references to use 40- or 64-digit hexadecimal
commit IDs, and `docker://` images to use digests. Local references and expression-based
references are skipped.

**policy.require-job-timeout**
: When `true`, require `timeout-minutes` on jobs that run steps. A mapping such as
`{ min-minutes: 5, max-minutes: 60 }` sets inclusive bounds for literal timeout values.
Either bound can be omitted; each must be finite and positive, and the minimum cannot exceed
the maximum. Jobs calling reusable workflows are skipped; expression-based timeouts are not compared.

**policy.require-permissions**
: When `true` or `{ scope: workflow }`, require a workflow-level `permissions` declaration.
Use `{ scope: job }` to require one on every job, including reusable workflow calls, even when
the workflow declares permissions. Empty mappings, named scopes, `read-all`, and `write-all`
satisfy the check. This policy checks declaration presence; it does not infer least privilege.

**policy.required-actions**
: List actions every workflow must use. Entries accept name and ref glob patterns, such as
`actions/checkout` or `my-org/security-scan@v2*`. Only steps directly in the workflow are
searched. Workflows consisting entirely of reusable workflow calls, or containing an
expression-based action reference, are skipped.

Policy checks are off until enabled. For example:

```yaml
self-hosted-runner: { labels: [linux.2xlarge] }
config-variables: [DEFAULT_RUNNER]
config-secrets: [DEPLOY_TOKEN]
assume-default-permissions: restricted
policy:
  require-commit-hash: true
  require-job-timeout: { min-minutes: 5, max-minutes: 60 }
  require-permissions: true
  required-actions: [actions/checkout]
paths:
  ".github/workflows/**/*.{yml,yaml}":
    ignore: ['shellcheck reported issue in this script: SC2086:.+']
```

The repository supplies `actionlint.schema.json` for editor completion and validation. See the
configuration document below for the schema URL and full matching rules.

# EXTERNAL LINTERS

ShellCheck checks supported shell scripts in `run:` steps; pyflakes checks Python scripts.
The executables must be available through `PATH` or the corresponding command flag. If a
command cannot be resolved, its integration is skipped; **--verbose** explains why.

    $ actionlint --shellcheck= --pyflakes=
    $ actionlint --shellcheck 'shellcheck -e SC2086'
    $ actionlint --pyflakes 'python3 -m pyflakes'

Command strings are parsed into an executable and arguments, not executed by a shell. Shell
pipes and redirections are not supported in these flags. Your arguments precede actionlint's
own arguments, so do not supply input filenames or override ShellCheck's output format.

actionlint invokes ShellCheck with `--norc` and JSON1 output, so `.shellcheckrc` is not read.
Use **--shellcheck** arguments or `SHELLCHECK_OPTS` for ShellCheck options. Filter pyflakes
diagnostics with **--ignore** or the configuration's `paths` entries.

# OUTPUT

Diagnostics go to standard output. Command usage, progress logs, and fatal errors go to standard
error. The default diagnostic includes the file, line, column, message, rule name, and source
snippet. **--oneline** omits the snippet; **--format** replaces the diagnostic presentation.

The Go template receives a sequence of errors. Each has `Message`, `Snippet`, `Kind`, `Filepath`,
`Line`, `Column`, and `EndColumn` fields. Line and column numbers start at 1. Custom template
functions include `json`, `replace`, `toPascalCase`, `allKinds`, and `getVersion`.

JSON:

    $ actionlint --json

JSON Lines:

    $ actionlint --output jsonl

Custom lines:

    $ actionlint --format '{{range .}}{{.Filepath}}:{{.Line}}:{{.Column}}: {{.Message}} [{{.Kind}}]\n{{end}}'

Backslash escapes such as `\n` in the format string are expanded before the template is parsed.

SARIF uses the bundled template and needs no separate template file:

    $ actionlint --output sarif > actionlint.sarif

The CLI's **--format** accepts template text. The GitHub Action's `format` input instead accepts
names such as `json` and `sarif`. Changing output format does not change the lint exit status.

Native JSON returns a versioned document with `schema_version` and `diagnostics`.
Each diagnostic contains `rule`, `message`, `path`, `start`, `end`, and an optional source-line
`snippet`. Positions use one-based Unicode character columns and exclusive end positions;
a range may end on a later line. A clean check writes `{"schema_version":1,"diagnostics":[]}`.
JSON Lines writes one diagnostic per line with `schema_version: 1` on every record, or nothing
for a clean check. The legacy `{{json .}}` template retains its array, field names, and inclusive
`end_column` contract.

In JSON modes, fatal errors are JSON objects on standard error with `error` and `exit_code`.
Requested progress or debug logs are separate JSON Lines records with a `log` field on standard
error. **--quiet** suppresses these log records. Standard output contains only the requested result.
An early failure can leave standard output empty; always check the exit status.

Machine-readable metadata:

    $ actionlint --help --json
    $ actionlint --version --json
    $ actionlint --init-config --json

JSON help describes flag names, shorthand, types, default spellings, groups, choices, repeatability,
legacy aliases, and exit codes. JSON version includes `name`, `version`, `installed_from`,
`go_version`, `os`, and `goarch`. JSON config creation returns `{"path":"..."}`.
The default three-line version format remains unchanged.

# SHELL COMPLETION

**--completion** generates native completion scripts for Bash, Fish, Zsh, and PowerShell. Cobra generates these scripts from the CLI flag definitions. Scripts
complete long flags, short options, flag values, and workflow paths. Bash requires the
`bash-completion` package; Zsh requires `compinit`. Load one into the current shell session:

Bash:

    $ source <(actionlint --completion bash)

Fish:

    $ actionlint --completion fish | source

Zsh, after initializing its completion system:

    $ autoload -Uz compinit && compinit
    $ source <(actionlint --completion zsh)

PowerShell:

```powershell
actionlint --completion powershell | Out-String | Invoke-Expression
```

For persistent installation, save Bash output in a directory loaded by bash-completion, Fish
output as `~/.config/fish/completions/actionlint.fish`, or Zsh output as `_actionlint` in a
directory on `fpath`. Load PowerShell output from your profile. Regenerate saved scripts after
upgrading actionlint so they reflect the installed CLI. See the usage document for setup examples.

# ENVIRONMENT

**PATH**
: Used to find ShellCheck and pyflakes, including the executable selected by their command flags.

**NO_COLOR**
: A nonempty value disables automatic color. **--color** can force color; **--no-color** always
disables it.

**SHELL**, **PSModulePath**
: Used by **--completion auto**. A supported shell named by `SHELL` takes precedence over the
PowerShell fallback indicated by a nonempty `PSModulePath`.

**SHELLCHECK_OPTS**
: Additional options interpreted by the external ShellCheck process.

# FILES

**.github/workflows/**
: Workflow directory scanned when no filenames are provided.

**.github/actionlint.yaml**, **.github/actionlint.yml**
: Optional repository configuration; the `.yaml` spelling takes precedence.

# DOCUMENTS

Detailed documentation for this release and current installation options are available online.

## Checks

https://github.com/kjanat/actionlint/blob/v1.16.1/docs/checks.md

Full list of all checks done by actionlint with example inputs, outputs, and playground links.

## Installation

https://github.com/kjanat/actionlint/blob/master/docs/install.md

Installation instructions for npm, Homebrew, AUR, Scoop, mise, release archives, the download
script, Docker, and Go, plus the status of WinGet and upstream-only package names.

## Usage

https://github.com/kjanat/actionlint/blob/v1.16.1/docs/usage.md

CLI usage, shell completion, output templates, the GitHub Action, Docker images, and editor
and CI integrations.

## Configuration

https://github.com/kjanat/actionlint/blob/v1.16.1/docs/config.md

Repository configuration, runner labels, variables, secrets, and opt-in policy checks.

## Go API

https://github.com/kjanat/actionlint/blob/v1.16.1/docs/api.md

How to use actionlint as Go library.

## References

https://github.com/kjanat/actionlint/blob/v1.16.1/docs/reference.md

Links to resources.

# USAGE ON GITHUB ACTIONS

The repository provides a GitHub Action with actionlint, ShellCheck, and pyflakes in a prebuilt
Docker image. It reports GitHub annotations by default and fails when problems are found:

```yaml
name: Lint GitHub Actions workflows
on: [push, pull_request]
permissions: { contents: read }

jobs:
  actionlint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with: { persist-credentials: false }
      - uses: kjanat/actionlint@v1
```

The Docker action requires a Linux runner with a reachable Docker daemon. To run the binary
directly, including on macOS, Windows with Bash, or a runner without a Docker daemon, use the
download script:

```yaml
- name: Check workflow files
  run: |
    bash <(curl -fsSL https://raw.githubusercontent.com/kjanat/actionlint/HEAD/scripts/download-actionlint.bash) latest
    ./actionlint --color
  shell: bash
```

The script accepts `latest` to resolve the newest release, or a specific version. Without a
version argument, it uses the default recorded in the script. It writes the executable to the
current directory and, on GitHub Actions, exposes its path as the
`executable` step output. External linters must be available separately when using the binary.

# EXIT STATUS

`actionlint` command exits with one of the following exit statuses.

- **0**: It ran successfully and no problem was found.
- **1**: It ran successfully and some problem was found.
- **2**: Command-line parsing or option validation failed, such as an unknown flag, missing value,
  unknown output mode, or incompatible output flags.
- **3**: Initialization or linting failed, for example because a file cannot be read, a project
  cannot be found, or a configuration, ignore pattern, or output template is invalid.

# PLAYGROUND

The WebAssembly playground runs actionlint in your browser. Workflow linting happens locally in
the browser; it does not execute workflows or run the external ShellCheck and pyflakes programs.

https://kjanat.github.io/actionlint/

Paste a workflow into the editor to see diagnostics update as you type. Select a diagnostic to
jump to its source position.

# BUGS

Report problems with this fork to its issue tracker. Include the output of **--version**, the
relevant configuration, and a minimal workflow that reproduces the problem.

https://github.com/kjanat/actionlint/issues

# COPYRIGHT

**actionlint** is licensed under the MIT License.

Copyright (c) 2026 Kaj Kowalski\
Copyright (c) 2021 rhysd

https://github.com/kjanat/actionlint/blob/HEAD/LICENSE.txt

<!-- markdownlint-disable-file code-block-style commands-show-output single-title -->
