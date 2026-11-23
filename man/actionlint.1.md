---
title: actionlint
section: 1
header: General Commands Manual
---

# NAME

**actionlint** - static checker for GitHub Actions workflow files

# SYNOPSIS

**actionlint** [*flags*] [**--**] [*files*... | **-**]\
**actionlint check** [*flags*] [**--**] [*files*... | **-**]

# DESCRIPTION

**actionlint** checks GitHub Actions workflow files without running them. It checks syntax,
expression types, action inputs and outputs, reusable workflows, runner labels, schedules and
script injection. ShellCheck and Ruff check shell and Python scripts through registry Wasm
plugins by default.

# USAGE

With no arguments inside a Git repository, **actionlint** checks the repository's workflow and
action files. Outside a repository, it prints help and exits 0. **actionlint check** exits 3
outside a repository or when it finds no inputs.

Pass files, directories, glob patterns or **-** for stdin. Quote glob patterns to let
actionlint expand them.

    $ actionlint check
    $ actionlint file1.yaml file2.yaml
    $ actionlint check file1.yaml --output-format=json
    $ actionlint check --stdin-filename .github/workflows/ci.yml -

Options can come before or after filenames. **--** makes later arguments filenames. Pass
`./NAME` to check a file named like a command.

# COMMANDS

**fix** [**--apply**] [**--include-suggested**] [*FILES...*]
: Preview edits as JSON. **--apply** writes validated edits and rechecks the changed files.

**lsp**
: Run the language server over stdin and stdout.

**cache warm** [*FILES...*]
: Download configuration, plugins and remote action metadata for offline checks.

**cache dir**, **cache list**
: Print the cache location, or list cached files.

**cache prune** [**--older-than** *DURATION*] [**--dry-run**]
: Remove cache files written more than 30 days ago.

**cache clean** [**--dry-run**]
: Remove the whole cache except plugin revocation records.

**plugin search** [*QUERY*], **plugin versions** *PLUGIN*
: Search the registry for plugins, or list the published tags of one plugin.

**plugin install** [*PLUGIN*] [**--version** *TAG|latest*]
: Add a plugin to the config's **plugins** list and download it. Without a name, the command
installs every declared plugin. The default version is the highest stable release. Prereleases
need an exact tag.

**plugin list**, **plugin update** [*PLUGIN*], **plugin remove** *PLUGIN*, **plugin sync**
: List, update or remove declared plugins, or download declared releases into the cache. Use
**--config** outside a repository.

**plugin inspect** *PATH-OR-URI*
: Read plugin metadata without running the plugin.

**check** [*flags*] [*files*...]
: Check workflows with the modern option grammar.

**config init** [**--json** | **--jsonc** | **--toml** | **--yaml** | **--yml**]
: Create a repository config. YAML is the default. The command refuses to overwrite an existing
config.

**config path**
: Print the selected configuration path.

**config show** [**--origin**]
: Show the effective configuration. **--origin** adds the source of each setting.

**config validate**
: Validate the configuration.

**config schema**
: Print the configuration JSON schema.

**rules** [*name*]
: List checks, or explain one check.

**doctor**
: Inspect configuration and tool resolution without running tools.

**completion** *shell*
: Print a shell completion script.

**version**
: Show version and build details.

# FLAGS

Options apply to the root and to **check** unless noted. Run **--help-legacy** for old
single-dash spellings.

**--resolve-with** *EXECUTABLE*
: Resolve remote dependencies with this executable.

**--resolver-arg** *VALUE*
: Pass an argument to the resolver. Repeatable.

**--offline**
: Block network requests and use the cache. A missing required config or plugin stops the
check with a warning and exit status 0. Native tools can still reach the network. Run
**actionlint cache warm** before going offline.

**--online**
: Allow network requests. Requests use `GH_TOKEN` or `GITHUB_TOKEN`.

**--output-format**, **--output**, **-o** *MODE*
: Select `text`, `pretty`, `oneline`, `gcc`, `json`, `jsonl`, `sarif` or `github`. The default
is `pretty`. Repeat the flag to emit several formats.

**--diagnostic-width** *COLUMNS*
: Wrap `pretty` diagnostics at 40 to 400 columns. Default: 100.

**--dependency-findings** *MODE*
: Show dependency findings in `compact` (default) or `full` detail.

**--json**
: Select JSON output. With help, version, config, rules or doctor, print JSON metadata.

**--json-pretty**[=*BOOL*]
: Format JSON and SARIF with jq on a terminal. Default: true. Explicit true also selects JSON
when no other format is set, and indents JSON in pipes and files.

**--template**, **--format**, **-f** *TEMPLATE*
: Render diagnostics with a Go template.

**--template-file** *PATH*
: Read a Go template from a file.

**--output-file** *PATH*
: Write results to *PATH*. `-` means stdout. actionlint asks before it replaces an existing
file.

**-y**, **--yes**
: Answer yes to prompts. Replacing a report needs **--yes** when stderr is not a terminal or
workflow input comes from stdin.

**--color**[=*MODE*]
: On **check**, select `auto`, `always` or `never`. A bare flag means `always`. On the root,
use boolean syntax such as `-color=false`.

**--no-color**
: Disable color.

**--hyperlinks** *MODE*
: Select `auto` (default), `always` or `never` for OSC 8 links in help and doctor.

**-C**, **--directory** *DIR*
: Run from *DIR*. Repeat to resolve each relative directory from the previous one.

**--config**, **--config-file** *PATH*
: Select the configuration file.

**--no-config**, **--no-rc**, **--norc**
: Use defaults without a configuration file.

**--no-ignore**
: Include Git-ignored files during discovery.

**--ignore-regex** *REGEXP*
: Suppress findings whose message or `CODE: message` matches this Go regular expression.
Repeatable.

**--ignore** *RULE*
: Ignore a *RULE*, diagnostic code or rule/code. Repeatable.

**--plugin** *PATH*
: Load a WebAssembly component plugin from a file or an
`oci://`*HOST*`/`*REPOSITORY*`@sha256:`*HEX* reference. Repeatable. Each analysis gets 64 MiB
of memory and 10 seconds.

**--runner-target** *TARGET*
: Select the runner execution target. Default: `github`.

**--runner-provider** *NAME*
: Select the runner provider for the target.

**--runner-snapshot** *FILE*
: Read runner information from a JSON snapshot of up to 8 MiB.

**--runner-scope** *SCOPE*
: Set the organization or repository scope for runner information. Default: the project root.

**--runner-service** *SERVICE*
: Select `github.com`, `github-data-residency`, `ghes` or `unknown`. This flag overrides
**repository.service**.

**--runner-service-version** *VERSION*
: Set the GHES version, such as `3.19`.

**--as-of** *DATE*
: Assess lifecycle events on `YYYY-MM-DD`. Default: today in UTC.

**--runner-as-of** *DATE*
: Alias for **--as-of**.

**--enforce-policy** *FILE* or *URL*
: Require lifecycle policies from a file or HTTPS URL. Repeatable.

**--stdin-filename** *PATH*
: Set the path for stdin diagnostics. A workflow path selects its repository. Default:
`<stdin>`.

**--log-level** *LEVEL*
: Select `none` (default), `info` or `debug`.

**--quiet**, **-q**
: Suppress progress, debug logs and summaries.

**--strict**
: Enable stable correctness, security and suspicious rules and fail on warnings and errors.
Explicit exceptions and suppressions still apply.

**--experimental**
: Enable nursery rules.

**--summary**
: Print file and finding counts on stderr. Default: true.

**--verbose**, **-v**
: Enable progress logging.

**--network-stats**
: Write network request statistics to stderr as a JSON record.

**--debug**
: Enable debug logging.

**--oneline**
: Print one diagnostic per line.

**--tool** *ID*[=*COMMAND*]
: Run the tool *ID*, such as `shellcheck` or `ruff`, and fail if it cannot run. `ID=COMMAND`
selects a custom command. `ID=false` disables the tool. Repeatable. The last **--tool** or
**--no-tool** for an ID wins.

**--no-tool** *ID*
: Disable the tool *ID*.

**--no-tools**
: Disable all external linters.

**--shellcheck** *COMMAND*
: Deprecated. **--shellcheck** works like **--tool** `shellcheck=COMMAND`.

**--pyflakes** *COMMAND*
: Deprecated. **--pyflakes** selects Ruff's Pyflakes rules. actionlint never runs the command.

**--allow-config-processes**
: Allow native executables from repository configuration. Without this flag,
`tools.<name>.backend.process` is an error.

**--init-config**, **--init**
: Aliases for **config init**.

**--completion**, **--completions** *SHELL*
: Root-only aliases for **completion** *SHELL*.

**--version**, **-V**
: Print the version. Root only. On **plugin install** and **plugin update**, **--version** takes
a release tag or `latest`.

**--help**, **-h**
: Show help on stderr.

**--help-legacy**
: Explain legacy option spellings.

# CONFIGURATION

A file can inherit settings through `extends`.

**repository.runners**
: Extra runner-label glob patterns.

**repository.variables**
: Names allowed in the `vars` context. `null` makes the set unknown.

**repository.secrets**
: Allowed secret names. Built-in secrets such as `GITHUB_TOKEN` are always allowed.

**repository.default-workflow-permissions**
: The permission, `read` or `write`, that actionlint assumes for a local reusable workflow call
without `permissions`.

**lint.message-ignores**
: Map path glob patterns to message regular expressions.

An inline comment suppresses a finding for the rule IDs it names:

```yaml
cache-mode: write # actionlint:ignore cache-write-untrusted -- jobs use reviewed default-branch code only
```

`# actionlint:ignore-next-line RULE -- REASON` on the line before a declaration works too.
Separate several rule IDs with commas or spaces.

```yaml
repository:
  runners: [linux.2xlarge]
  variables: { DEFAULT_RUNNER: null }
  secrets: [DEPLOY_TOKEN]
  default-workflow-permissions: read
lint:
  rules:
    security:
      unpinned-reference: on
    conventions:
      job-timeout:
        level: on
        options: { min-minutes: 5, max-minutes: 60 }
```

# EXTERNAL LINTERS

ShellCheck checks `bash`, `sh`, `dash` and `ksh` scripts in `run:` steps.

    $ actionlint --no-tool shellcheck
    $ actionlint --tool shellcheck='shellcheck -e SC2086'

No shell runs the command. Your arguments precede actionlint's own. Do not pass input files or
change ShellCheck's output format.

actionlint passes `--norc` to ShellCheck. Set `tools.shellcheck.config` to inline settings, an
rc file, or a directory that contains one.

# OUTPUT

Diagnostics go to stdout. Usage, logs and fatal errors go to stderr. The output format does not
change the exit status.

Templates receive a list of errors with `Message`, `Snippet`, `Kind`, `Filepath`, `Line`,
`Column` and `EndColumn` fields.

    $ actionlint --json
    $ actionlint --output jsonl
    $ actionlint --format '{{range .}}{{.Filepath}}:{{.Line}}:{{.Column}}: {{.Message}} [{{.Kind}}]\n{{end}}'
    $ actionlint --output sarif > actionlint.sarif

Each JSON diagnostic has `rule`, `message`, `path`, `start` and `end`. Columns count Unicode
characters from 1, and end positions are exclusive. A clean single-file check includes these
fields:

```json
{"schema_version":1,"status":"success","completed":true,"exit_code":0,"file_count":1,"diagnostics":[],"hints":[]}
```

See [Analysis results](https://github.com/kjanat/actionlint/blob/HEAD/docs/results.md) for the
schema. An incomplete check writes its JSON result to stderr with `completed: false`. Check the
exit status. An empty `diagnostics` array alone does not mean success.

Machine-readable metadata:

    $ actionlint --help --json
    $ actionlint --version --json
    $ actionlint --init-config --json

# SHELL COMPLETION

Bash needs the `bash-completion` package. Regenerate saved scripts after upgrading actionlint.

Bash:

    $ source <(actionlint --completion bash)

Fish:

    $ actionlint --completion fish | source

Zsh:

    $ autoload -Uz compinit && compinit
    $ source <(actionlint --completion zsh)

PowerShell:

```powershell
actionlint --completion powershell | Out-String | Invoke-Expression
```

# ENVIRONMENT

**ACTIONLINT_CONFIG**, **ACTIONLINT_NO_CONFIG**, **ACTIONLINT_CONFIG_ORIGIN**,
**ACTIONLINT_STDIN_FILENAME**, **ACTIONLINT_IGNORE_REGEX**, **ACTIONLINT_OUTPUT_FORMAT**,
**ACTIONLINT_JSON**, **ACTIONLINT_JSON_PRETTY**, **ACTIONLINT_TEMPLATE**,
**ACTIONLINT_TEMPLATE_FILE**, **ACTIONLINT_OUTPUT_FILE**, **ACTIONLINT_LOG_LEVEL**,
**ACTIONLINT_QUIET**, **ACTIONLINT_SUMMARY**, **ACTIONLINT_COLOR**, **ACTIONLINT_HYPERLINKS**
: Defaults for the matching flags. Explicit flags win. **ACTIONLINT_IGNORE_REGEX** is a JSON
string array.

`ACTIONLINT_TOOL_<ID>_BIN`, `ACTIONLINT_TOOL_<ID>_FLAGS`, `ACTIONLINT_TOOL_<ID>_ENV`
: The executable, extra arguments and child environment for the tool *ID*, such as
`ACTIONLINT_TOOL_SHELLCHECK_BIN`. An empty `_BIN` disables the tool.

**NO_COLOR**
: A nonempty value, including `0`, disables automatic color.

**NO_HYPERLINKS**, **FORCE_HYPERLINKS**
: A nonempty value disables or forces links in `auto` mode.

**GITHUB_ACTIONS**
: The value `true` enables color in workflow logs.

**SHELL**, **PSModulePath**
: **--completion auto** reads these variables to detect the shell.

**SHELLCHECK_OPTS**
: ShellCheck reads extra options from this variable.

# FILES

**.github/actionlint.yaml**, **.github/actionlint.yml**, **.github/actionlint.json**, **.github/actionlint.jsonc**, **.github/actionlint.toml**
: Repository configuration. actionlint reads the first file that exists, in this order.

# DOCUMENTS

**Checks**
: https://github.com/kjanat/actionlint/blob/HEAD/docs/checks.md

**Installation**
: https://github.com/kjanat/actionlint/blob/master/docs/install.md

**Usage**
: https://github.com/kjanat/actionlint/blob/HEAD/docs/usage.md

**Configuration**
: https://github.com/kjanat/actionlint/blob/HEAD/docs/config.md

**API**
: https://github.com/kjanat/actionlint/blob/HEAD/docs/api.md

**References**
: https://github.com/kjanat/actionlint/blob/HEAD/docs/reference.md

# USAGE ON GITHUB ACTIONS

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

To run the binary directly, including on macOS or on Windows with Bash, use the download script:

```yaml
- name: Check workflow files
  run: |
    bash <(curl -fsSL https://raw.githubusercontent.com/kjanat/actionlint/662318dd6bbd9c0c120e35b03168bc1be69bf428/scripts/download-actionlint.bash) latest
    ./actionlint --color
  shell: bash
```

The script takes `latest` or a version.

# EXIT STATUS

- **0**: The command succeeded, and no finding reached its failure threshold.
- **1**: At least one finding reached its failure threshold. Errors fail a check by default.
  With **--strict**, warnings fail it too.
- **2**: The command line is invalid, such as an unknown flag or conflicting output flags.
- **3**: Initialization or linting failed, such as an unreadable file or invalid configuration.

# BUGS

Include the **--version** output, your configuration and a minimal workflow in bug reports.

https://github.com/kjanat/actionlint/issues

# COPYRIGHT

**actionlint** is licensed under the MIT License.

Copyright (c) 2026 Kaj Kowalski\
Copyright (c) 2021 rhysd

https://github.com/kjanat/actionlint/blob/HEAD/LICENSE.txt

<!-- markdownlint-disable-file code-block-style commands-show-output single-title -->
