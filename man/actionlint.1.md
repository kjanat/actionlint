---
title: actionlint
section: 1
header: General Commands Manual
footer: actionlint 1.16.0
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

With no file arguments, **actionlint** searches the current directory and its parents for a
repository containing both `.git` and `.github/workflows`. It recursively checks `.yml` and
`.yaml` files in that workflow directory:

    $ actionlint

To check specific workflow files, pass their paths. Explicit files can be outside a repository:

    $ actionlint file1.yaml file2.yaml

Pass **-** as the only file argument to read a workflow from standard input:

    $ actionlint -

Use **-stdin-filename** to label diagnostics. If that path exists in a detected repository,
actionlint also uses its repository configuration and local action metadata. For an unsaved
file, select configuration explicitly with **-config-file** when needed:

    $ actionlint -stdin-filename .github/workflows/ci.yml - < .github/workflows/ci.yml
    $ actionlint -config-file .github/actionlint.yaml - < /tmp/workflow.yml

Local action metadata and reusable workflows are read from disk when referenced by a workflow.
Passing an `action.yml` file directly does not select a standalone action-metadata lint mode.

Place flags before file arguments. Both `-flag` and `--flag` spellings are accepted, and a value
can follow a flag or an equals sign. Use `--` to end flag parsing before a filename that starts
with a dash. Boolean flags accept `=true` or `=false`.

Use **-format** to serialize diagnostics or customize their presentation with Go templates:

    $ actionlint -format '{{json .}}'

# FLAGS

**-color**
: Force colored output, including when standard output is redirected. **-no-color** takes
precedence if both flags are set.

**-completion** *SHELL*
: Print a shell completion script for the given shell to stdout. One of `bash`, `fish`,
`powershell`, `zsh`. Also accepted are `pwsh`, a shell path such as `$SHELL`, and `auto` to
detect the current shell from `SHELL`, falling back to PowerShell when `PSModulePath` is set.
Prints the script and exits without linting. See SHELL COMPLETION below.

**-config-file** *PATH*
: Read configuration from *PATH* instead of using the detected repository's configuration.
Relative paths are resolved from the current working directory.

**-debug**
: Write development diagnostics to standard error. Use without **-verbose**, which takes
precedence when both flags are set.

**-format** *FORMAT*
: Format diagnostics using a Go text template. The template receives a sequence of error
objects. See OUTPUT below. This overrides **-oneline**.

**-ignore** *PATTERN*
: Suppress diagnostics whose message matches *PATTERN*, using Go regular expression syntax.
Repeat the flag to match any of several patterns: `-ignore A -ignore B` suppresses messages
matching either pattern. Suppressed diagnostics do not cause exit status 1.

**-init-config**
: Create `.github/actionlint.yaml` in the detected repository and exit without linting.
Includes the YAML Language Server schema directive for editor completion, hover documentation, and validation.
Requires a repository with `.github/workflows` and refuses to overwrite either supported
configuration filename.

**-no-color**
: Disable colored output, even when **-color** is also set.

**-oneline**
: Print one line per diagnostic, without the source snippet and position marker.

**-pyflakes** *COMMAND*
: Command used to check Python `run:` scripts. Accepts an executable name, a path, or a quoted
command line such as `"python3 -m pyflakes"` or `"uvx pyflakes"`. Defaults to `pyflakes`;
`-pyflakes=` disables the integration.

**-shellcheck** *COMMAND*
: Command used to check supported shell `run:` scripts. Accepts an executable name, a path,
or a quoted command line such as `"shellcheck -e SC2086"`. Defaults to `shellcheck`;
`-shellcheck=` disables the integration.

**-stdin-filename** *NAME*
: Filename used for standard-input diagnostics. Defaults to `<stdin>`. An existing path also
allows repository discovery; see USAGE above.

**-verbose**
: Write progress information to standard error, including file discovery and disabled external
linter integrations.

**-version**
: Print the build's module name and version, installation source, Go compiler version, and
target operating system and architecture, then exit.

**-help**, **-h**
: Print command usage and flags to standard error, then exit successfully.

# CONFIGURATION

Configuration is optional. In a detected repository, actionlint reads `.github/actionlint.yaml`,
or `.github/actionlint.yml` if the first filename is absent. **-config-file** selects a different
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
expressions, applied in addition to **-ignore**.

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
command cannot be resolved, its integration is skipped; **-verbose** explains why.

    $ actionlint -shellcheck= -pyflakes=
    $ actionlint -shellcheck 'shellcheck -e SC2086'
    $ actionlint -pyflakes 'python3 -m pyflakes'

Command strings are parsed into an executable and arguments, not executed by a shell. Shell
pipes and redirections are not supported in these flags. Your arguments precede actionlint's
own arguments, so do not supply input filenames or override ShellCheck's output format.

actionlint invokes ShellCheck with `--norc` and JSON1 output, so `.shellcheckrc` is not read.
Use **-shellcheck** arguments or `SHELLCHECK_OPTS` for ShellCheck options. Filter pyflakes
diagnostics with **-ignore** or the configuration's `paths` entries.

# OUTPUT

Diagnostics go to standard output. Command usage, progress logs, and fatal errors go to standard
error. The default diagnostic includes the file, line, column, message, rule name, and source
snippet. **-oneline** omits the snippet; **-format** replaces the diagnostic presentation.

The Go template receives a sequence of errors. Each has `Message`, `Snippet`, `Kind`, `Filepath`,
`Line`, `Column`, and `EndColumn` fields. Line and column numbers start at 1. Custom template
functions include `json`, `replace`, `toPascalCase`, `allKinds`, and `getVersion`.

JSON:

    $ actionlint -format '{{json .}}'

JSON Lines:

    $ actionlint -format '{{range .}}{{json .}}{{end}}'

Custom lines:

    $ actionlint -format '{{range .}}{{.Filepath}}:{{.Line}}:{{.Column}}: {{.Message}} [{{.Kind}}]\n{{end}}'

Backslash escapes such as `\n` in the format string are expanded before the template is parsed.

For SARIF, pass the contents of `sarif_template.txt` from the actionlint source repository to
**-format**, for example in Bash:

    $ actionlint -format "$(cat sarif_template.txt)" > actionlint.sarif

The CLI's **-format** accepts template text. The GitHub Action's `format` input instead accepts
names such as `json` and `sarif`. Changing output format does not change the lint exit status.

# SHELL COMPLETION

**-completion** generates native completion scripts for Bash, Fish, Zsh, and PowerShell. Scripts
complete flags, flag values, and workflow paths. Load one into the current shell session:

Bash:

    $ source <(actionlint -completion bash)

Fish:

    $ actionlint -completion fish | source

Zsh, after initializing its completion system:

    $ autoload -Uz compinit && compinit
    $ source <(actionlint -completion zsh)

PowerShell:

```powershell
actionlint -completion powershell | Out-String | Invoke-Expression
```

For persistent installation, save Bash output in a directory loaded by bash-completion, Fish
output as `~/.config/fish/completions/actionlint.fish`, or Zsh output as `_actionlint` in a
directory on `fpath`. Load PowerShell output from your profile. Regenerate saved scripts after
upgrading actionlint so they reflect the installed CLI. See the usage document for setup examples.

# ENVIRONMENT

**PATH**
: Used to find ShellCheck and pyflakes, including the executable selected by their command flags.

**NO_COLOR**
: A nonempty value disables automatic color. **-color** can force color; **-no-color** always
disables it.

**SHELL**, **PSModulePath**
: Used by **-completion auto**. A supported shell named by `SHELL` takes precedence over the
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

https://github.com/kjanat/actionlint/blob/v1.16.0/docs/checks.md

Full list of all checks done by actionlint with example inputs, outputs, and playground links.

## Installation

https://github.com/kjanat/actionlint/blob/master/docs/install.md

Installation instructions for npm, Homebrew, AUR, Scoop, mise, release archives, the download
script, Docker, and Go, plus the status of WinGet and upstream-only package names.

## Usage

https://github.com/kjanat/actionlint/blob/v1.16.0/docs/usage.md

CLI usage, shell completion, output templates, the GitHub Action, Docker images, and editor
and CI integrations.

## Configuration

https://github.com/kjanat/actionlint/blob/v1.16.0/docs/config.md

Repository configuration, runner labels, variables, secrets, and opt-in policy checks.

## Go API

https://github.com/kjanat/actionlint/blob/v1.16.0/docs/api.md

How to use actionlint as Go library.

## References

https://github.com/kjanat/actionlint/blob/v1.16.0/docs/reference.md

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
    ./actionlint -color
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
- **2**: Command-line flag parsing failed, for example because of an unknown flag or missing value.
- **3**: Initialization or linting failed, for example because a file cannot be read, a project
  cannot be found, or a configuration, ignore pattern, or output template is invalid.

# PLAYGROUND

The WebAssembly playground runs actionlint in your browser. Workflow linting happens locally in
the browser; it does not execute workflows or run the external ShellCheck and pyflakes programs.

https://kjanat.github.io/actionlint/

Paste a workflow into the editor to see diagnostics update as you type. Select a diagnostic to
jump to its source position.

# BUGS

Report problems with this fork to its issue tracker. Include the output of **-version**, the
relevant configuration, and a minimal workflow that reproduces the problem.

https://github.com/kjanat/actionlint/issues

# COPYRIGHT

**actionlint** is licensed under the MIT License.

Copyright (c) 2026 Kaj Kowalski\
Copyright (c) 2021 rhysd

https://github.com/kjanat/actionlint/blob/HEAD/LICENSE.txt

<!-- markdownlint-disable-file code-block-style commands-show-output single-title -->
