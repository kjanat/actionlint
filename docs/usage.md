# Usage

[![GitHub Release][release-badge]][releases]

This document describes how to use [actionlint](../README.md).

## `actionlint` command

With no argument, actionlint finds all workflow files in the current repository
and checks them.

```sh
actionlint
```

When paths to YAML workflow files are given as arguments, actionlint checks them.

```sh
actionlint path/to/workflow1.yaml path/to/workflow2.yaml
```

When `-` argument is given, actionlint reads inputs from stdin and checks it as
workflow source.

```sh
cat path/to/workflow.yaml | actionlint -
```

To know all flags and options, see an output of `actionlint -h` or
[the online command manual][cmd-manual].

### Commands and compatibility

The root invocation remains supported. It uses the original Go flag grammar:
`-format` and `--format` are equivalent, boolean options accept `=false`, and
parsing stops at the first filename. The explicit `check` command accepts options
before or after filenames. Both routes use the same analysis engine.

```sh
actionlint
actionlint workflow.yml
actionlint check workflow.yml --output-format=json
actionlint check --stdin-filename=.github/workflows/ci.yml -
actionlint config show --origin
actionlint rules expression
actionlint doctor
actionlint version
```

`check` accepts workflow files or a lone `-` for stdin. With no paths, it uses the
same repository workflow discovery as the root. Directory arguments and standalone
action-manifest linting are not added by the command redesign.

An existing regular file named `check`, `config`, `rules`, `doctor`, `completion`,
`version` or `help` takes precedence over the corresponding root command. Use `--`
to force filenames or `--command NAME` to force a command despite a matching file.
Root options still belong before filenames:

```sh
actionlint -- --workflow.yml
actionlint --command check workflow.yml --json
```

Normal help and completion recommend the modern names. `--help-legacy` lists the
supported old names without deprecation warnings. `--format` remains an alias for
Go templates; it never interprets template text as a built-in format name.
`-o`, `-f`, `-q` and `-h` mean output format, template, quiet and help.
`-v` remains available for progress logging; new examples use `--log-level=info`.
`-V` is the short form of `--version` and preserves its three-line output.
Attached short-option values such as `-ojson` belong to `check`'s modern grammar.

The root's `-color=false` means not forcing color on. If both `-color` and
`-no-color` are true, `-no-color` wins regardless of order. The modern command
accepts `check --color=auto|always|never`; bare `--color` means `always`.

Human-readable help styles headings, command names and flags when stderr is a
terminal. With `GITHUB_ACTIONS=true`, help and text diagnostics also use color in
workflow logs without a terminal, including when `TERM=dumb`. Redirects to regular
files and `--output-file` reports stay plain in auto mode. Custom templates receive
no added color. JSON can use optional terminal formatting described below.
Following the [NO_COLOR convention](https://no-color.org/),
any non-empty `NO_COLOR` value, including `0` or `false`, disables automatic color;
an empty value has no effect. `TERM=dumb` disables automatic help styling outside GitHub Actions.
An explicit color request overrides those environment settings.
`--no-color` and `check --color=never` disable styling. Root color flags belong
before `--help`, since root help exits immediately. Redirected JSON help is uncolored.

Help can make the project name and documentation URLs clickable using OSC 8
terminal hyperlinks. The documentation URL uses the release tag or the build's
source commit. Without embedded version or VCS metadata, it falls back to `HEAD`;
use `go run -buildvcs=true ./cmd/actionlint --help` to include the checkout revision
when running from source.

Doctor uses `file://` links for its directory, configuration
file and resolved executables. Select `--hyperlinks=auto|always|never` independently of
color. The default `auto` follows the [no-hyperlinks convention](https://no-hyperlinks.org/spec):
non-empty `NO_HYPERLINKS` disables links, then non-empty `FORCE_HYPERLINKS`
enables them, then the output stream's TTY status and known terminal capabilities decide:
stderr for help, stdout for doctor.
Even `0` is a non-empty value. Explicit `always` or `never` overrides both variables.
Unknown terminals and multiplexers use plain URLs in auto mode; `always` can
enable links when the terminal and multiplexer are configured to pass them through.
Both modes keep destination URLs visible. JSON, diagnostics, templates, version
output and generated completion scripts remain free of added hyperlink sequences.

```sh
actionlint --hyperlinks=always --help
actionlint check --hyperlinks=never --help
actionlint doctor --hyperlinks=always
```

Put root hyperlink flags before `--help`, just like color flags. The same modes
are available in subcommand help and generated shell completions.

When running through npm, these forms pass the help flag to actionlint:

```sh
npx @kjanat/actionlint --help
npm exec --package=@kjanat/actionlint -- actionlint --help
```

Do not put an extra `--` after the package in the first form. It reaches actionlint
as the end of options, making the following `--help` a filename.

### Output and machine-readable metadata

```sh
actionlint check --json
actionlint check --output-format=jsonl
actionlint check --output-format=sarif --output-file=actionlint.sarif
actionlint check --output-format=github
actionlint check --template-file=report.tmpl
actionlint check --help --json
actionlint version --json
```

`--output-format` accepts `text`, `oneline`, `json`, `jsonl`, `sarif` and `github`.
`--json` selects JSON. `--template` and its supported `--format` alias render the
existing Go-template fields and functions. `--template-file` reads the same syntax
from a file. A template cannot be combined with a built-in output selector.
The legacy `-oneline` option still yields to a nonempty template.

JSON returns a versioned document. Each diagnostic has `rule`, `message`, `path`,
`start`, `end` and an optional `snippet`. Positions use one-based Unicode character
columns, and the end position is exclusive. Ranges may span multiple lines. For example:

```json
{
  "schema_version": 1,
  "diagnostics": [
    {
      "rule": "expression",
      "message": "undefined variable \"missing\"",
      "path": ".github/workflows/ci.yml",
      "start": { "line": 6, "column": 24 },
      "end": { "line": 6, "column": 36 }
    }
  ]
}
```

A clean JSON result has an empty `diagnostics` array. JSON Lines emits those
individual diagnostic objects with `schema_version: 1` on every record, or nothing for a clean result. Legacy
`-format '{{json .}}'` retains its original array and field names, including
`kind`, `filepath` and `end_column`. SARIF uses the bundled renderer. `github`
emits escaped workflow annotation commands; it is enabled only by that explicit
format choice, including when running inside GitHub Actions.

Check results go to stdout, or to `--output-file PATH`. `--output-file -` means stdout.
This destination option applies only to checks.
A report file is replaced only after analysis completes; an operational failure
preserves any previous report. An output file cannot replace a consumed workflow, local action, reusable workflow, configuration, template, or existing stdin filename, including links to those files. Replacing a report preserves its permission bits; a new report is private to the current user.
Logs and operational errors go to stderr. Structured output never includes ANSI
color or terminal hyperlinks. A clean default text run stays silent.

`--log-level=none|info|debug` controls logging. `--summary` adds an opt-in count on
stderr. `--quiet` suppresses logs and summaries while preserving requested results
and errors. In JSON modes, stderr errors have `error` and `exit_code` fields, log
records have `log`, and the summary has `summary.files` and `summary.findings`.
Check the exit status before interpreting empty stdout as success.

| Exit status | Meaning                                                                       |
| ----------- | ----------------------------------------------------------------------------- |
| `0`         | Completed without findings, or completed an information/configuration command |
| `1`         | Completed with findings                                                       |
| `2`         | Invalid CLI arguments or incompatible options                                 |
| `3`         | Could not initialize or complete, including invalid config, regex or template |

`--help --json` describes commands, flags, choices, defaults and exit codes.
`version --json` exposes build metadata. The old `-version` and `--version` flags
retain their original three-line text output.

When stdout is a terminal and `jq` is installed, JSON metadata and JSON/SARIF
diagnostics are indented and colored according to the color controls. Use
`--json-pretty=false` to disable this. Pipes, files, JSONL, templates and stderr
records retain their existing output. Missing or failing `jq` falls back to the
original JSON; it is never installed automatically.

CLI defaults can also come from [environment variables](env.md), including
configuration selection, output, logging and external linters. Explicit flags
take precedence, including empty strings and `false`.

### Configuration inspection

```sh
actionlint check --config=other.yml workflow.yml
actionlint check --no-config workflow.yml
actionlint config init
actionlint config path
actionlint config show --origin
actionlint config validate
```

Configuration selection remains one explicit file or the repository's
`.github/actionlint.yaml`, then `.github/actionlint.yml`. There is no new global
configuration search or merge policy. `ACTIONLINT_CONFIG` and
`ACTIONLINT_NO_CONFIG` provide selection defaults; explicit config flags override
both. `--no-config` skips
configuration loading. An explicit `check --config` also avoids parsing an
unselected repository config.

`config path` prints the selected path, or nothing when none is selected. It can
identify an invalid file without parsing it. `config show` prints effective YAML;
`--json` returns a document with `path` and `config`. `--origin` adds an `origins`
map keyed by JSON Pointer. Sources distinguish `default` from `config`, and
states distinguish missing keys, explicit `null` and explicit values such as
`false` and `[]`. Config sources include line and column positions. A default
value does not imply that the user explicitly set it.

`config validate` uses the same parser and validation as analysis, including its
current handling of unknown keys described in [Configuration](config.md). It does
not silently introduce the stricter JSON Schema validation policy.
`config init` retains the repository destination, refuses to overwrite either
config filename, and includes the YAML Language Server schema directive.

`rules` lists checks by name, description and category; `rules NAME` explains one
check. `doctor` reports the selected config and resolves configured external-tool
commands without executing them. Its paths become clickable `file://` links when
hyperlinks are enabled, with spaces and non-ASCII characters encoded in the target.
Windows drive and UNC paths are supported. Missing optional tools are reported as unavailable;
a malformed selected configuration returns exit status 3. Help, version, rules and
completion do not load configuration or create a linter.

### Shell completion

`completion <shell>` prints a completion script for the given shell to stdout. The
supported shells are `bash`, `fish`, `powershell` and `zsh`. The value also
accepts `pwsh` as an alias for `powershell` and executable paths such as `$SHELL`.
`auto` selects the shell from `$SHELL`, then tries PowerShell when `$PSModulePath`
is set. Cobra generates the scripts from the CLI definitions. They complete long flags,
short options, values, and workflow paths. Install `bash-completion` for Bash,
and initialize `compinit` for Zsh. Regenerate saved scripts after upgrading.

```sh
mkdir -p ~/.local/share/bash-completion/completions
actionlint completion bash > ~/.local/share/bash-completion/completions/actionlint
```

```sh
mkdir -p ~/.config/fish/completions
actionlint completion fish > ~/.config/fish/completions/actionlint.fish
```

The zsh script belongs in a directory listed in `$fpath`.

```sh
actionlint completion zsh > "${fpath[1]}/_actionlint"
```

The PowerShell script is loaded from your profile. The first command creates
the profile's directory, which does not exist on a fresh account and makes
`Out-File` fail.

```powershell
New-Item -ItemType Directory -Force (Split-Path -Parent $PROFILE) | Out-Null
actionlint completion powershell | Out-File -Append -Encoding utf8 $PROFILE
```

To load the script into the current session only, pipe it through
`Invoke-Expression` instead.

```powershell
actionlint completion powershell | Out-String | Invoke-Expression
```

### Ignore some errors

The three cache policies support [inline exceptions](config.md#inline-cache-policy-exceptions) with a rule name and
a reason. Place the comment on the reported line, or use `actionlint:ignore-next-line` immediately before it:

```yaml
cache-mode: write # actionlint:ignore cache-write-untrusted -- this job runs reviewed default-branch code only
```

To ignore some errors, `-ignore` option offers to filter errors by messages
using regular expression. The option is repeatable. The regular expression
syntax is the same as [RE2][re2].

```sh
actionlint -ignore 'label ".+" is unknown' -ignore '".+" is potentially untrusted'
```

`-shellcheck` and `-pyflakes` take a command line, not only a path. A command
name, a file path, or a command with flags all work. Setting an empty string
disables the `shellcheck` and `pyflakes` rules. As a bonus, disabling them makes
actionlint much faster. These external linter integrations spawn many processes.

```sh
actionlint -shellcheck= -pyflakes=
actionlint -shellcheck 'shellcheck -e SC2086'
actionlint -pyflakes 'python3 -m pyflakes'
```

To configure executables, arguments and child environments separately, use
`ACTIONLINT_SHELLCHECK_BIN`, `ACTIONLINT_SHELLCHECK_FLAGS`, and
`ACTIONLINT_SHELLCHECK_ENV`, or their `ACTIONLINT_PYFLAKES_*` equivalents.
An explicit tool flag overrides all three environment settings for that tool.
See [External linter environment settings](env.md#external-linters) for quoting,
Windows paths and examples.

Your arguments are prepended to the ones actionlint appends itself, so do not
pass `-f`/`--format` or file arguments. actionlint appends
`--norc -f json1 -x --shell <dialect> -e SC1091,SC2194,SC2050,SC2153,SC2154,SC2157,SC2043 -`
and parses the JSON1 output.

By default, `--norc` disables rc discovery. Set
[`tools.shellcheck.config`](config.md#shellcheck) to an inline mapping or an rc
file/directory; an rc path replaces `--norc` with `--rcfile`. The dialect follows
the workflow's shell settings. Following sourced files (`-x`) can be disabled in
configuration and is disabled when the step's working directory cannot be resolved
locally. Additional options can use `-shellcheck '<command line>'` or the
[`SHELLCHECK_OPTS` environment variable](checks.md#check-shellcheck-integ). pyflakes has no
configuration file and no `# noqa`, so suppress its findings with `-ignore` or
the `paths:` section of the configuration file.

<a id="format"></a>

### Format error messages

`-format` option can flexibly format error messages with [Go template syntax][go-template].

Before explaining the formatting details, let's see some examples.

#### Example: Legacy template JSON

```sh
actionlint -format '{{json .}}'
```

This returns the original template-field array. Use `check --json` for the
versioned native result described above.

#### Example: Markdown

````sh
actionlint -format '
{{range $err := .}}### Error at line {{$err.Line}}, col {{$err.Column}} of `{{$err.Filepath}}`

{{$err.Message}}

```plaintext
{{$err.Snippet}}
```

{{end}}
'
````

Output:

<!-- dprint-ignore-start -->

````markdown
### Error at line 21, col 20 of `test.yaml`

property "platform" is not defined in object type {os: string}

```plaintext
          key: ${{ matrix.platform }}-node-${{ hashFiles('**/package-lock.json') }}
                   ^~~~~~~~~~~~~~~
```
````

<!-- dprint-ignore-end -->

#### Example: Serialized in [JSON Lines][jsonl]

```sh
actionlint --output jsonl
```

Output:

```text
{"message":"unexpected key \"branch\" for ...
{"message":"character '\\' is invalid for branch ...
{"message":"label \"linux-latest\" is unknown. ...
```

#### Example: [Error annotation][ga-annotate-error] on GitHub Actions

````sh
actionlint -format '
{{range $err := .}}::error file={{$err.Filepath}},line={{$err.Line}},col={{$err.Column}}::{{$err.Message}}%0A```%0A{{replace $err.Snippet "\\n" "%0A"}}%0A```
{{end}}
' -ignore 'SC2016:'
````

Output:

<img src="https://cdn.jsdelivr.net/gh/rhysd/ss@5530c2526b44ad28dc12f91a3d71bcd57940f008/actionlint/ga-annotate.png" alt="annotations on GitHub Actions" width="731" height="522"/>

To include newlines in the annotation body, it prints `%0A`. (ref
actions/toolkit#193). And it
suppresses `SC2016` shellcheck rule error since it complains about the template
argument.

Basically it is more recommended to use [Problem Matchers](#problem-matchers) or
reviewdog as explained in ['Tools integration' section](#tools-integ) below.

#### Example: [SARIF format][sarif]

[The Static Analysis Results Interchange Format (SARIF)][sarif] is a standardized format for the results of static analysis tools.

Use the built-in format directly:

```sh
actionlint --output sarif > actionlint.sarif
```

The [canonical template](../sarif_template.txt) remains available for custom formatting.

Outputs are also too large to be written here. Please read [the output example in test data](../testdata/format/test.sarif).

#### Formatting syntax

In [Go template syntax][go-template], `.` within `{{ }}` means the target object. Here, the target object is a sequence of error
objects.

The sequence can be traversed with `range` action, which is like `for ... = range ... {}` in Go.

```text
{{range $err := .}} this part iterates error objects with the iteration variable $err {{end}}
```

The error object has the following fields.

<!-- dprint-ignore-start -->

| Field                | Description                                           | Example                                                             |
| -------------------- | ----------------------------------------------------- | ------------------------------------------------------------------- |
| `{{$err.Message}}`   | Body of error message                                 | `property "platform" is not defined in object type {os: string}`    |
| `{{$err.Snippet}}`   | Code snippet to indicate error position               |  <code>          node_version: 16.x\n          ^~~~~~~~~~~~~</code> |
| `{{$err.Kind}}`      | Name of rule the error belongs to                     | `expression`                                                        |
| `{{$err.Filepath}}`  | Canonical relative file path of the error position    | `.github/workflows/ci.yml`                                          |
| `{{$err.Line}}`      | Line number of the error position (1-based)           | `9`                                                                 |
| `{{$err.Column}}`    | Column number of the error's start position (1-based) | `11`                                                                |
| `{{$err.EndColumn}}` | Column number of the error's end position (1-based)   | `23`                                                                |

<!-- dprint-ignore-end -->

Functions called in `{{ }}` placeholder are template actions. There are many
actions defined by Go standard library. In addition, there are a few custom
actions defined by actionlint. Most useful action would be `json` as we already
used it in the above JSON example. List of all custom actions are as follows:

| Action           | Description                                                                      | Example usage                             |
| ---------------- | -------------------------------------------------------------------------------- | ----------------------------------------- |
| `json x`         | Serialize `x` as JSON string followed by newline character                       | `{{json $err}}`                           |
| `replace x y z`  | Replace string `y` with `z` in `x`                                               | `{{replace $err.Filepath "\\" "/"}}`      |
| `toPascalCase x` | Convert `x` into PascalCase (e.g. 'foo-bar' to 'FooBar')                         | `{{toPascalCase $err.Kind}}`              |
| `allKinds`       | Return an array of kind objects. The kind object is explained in the below table | `{{range $ = allKinds}}{{$.Name}}{{end}}` |
| `getVersion`     | Return the version of actionlint as string                                       | `{{getVersion}}`                          |

The kind object returned from `allKinds` action has the following fields.

| Field                   | Description                   | Example                                     |
| ----------------------- | ----------------------------- | ------------------------------------------- |
| `{{$kind.Name}}`        | Name of the kind              | `syntax-check`                              |
| `{{$kind.Description}}` | Short description of the kind | `Checks for GitHub Actions workflow syntax` |

For example, the following simple iteration body

```text
line is {{$err.Line}}, col is {{$err.Column}}, message is {{$err.Message | printf "%q"}}
```

will produce output like below.

```text
line is 21, col is 20, message is "property \"platform\" is not defined in object type {os: string}"
```

In `{{ }}` placeholder, input can be piped and action can be used to transform
texts. In above example, the message is piped with `|` and transformed with
`printf "%q"`.

Note that special characters escaped with backslash like `\n` in the format
string are automatically unescaped.

### Exit status

`actionlint` command exits with one of the following exit statuses.

| Status | Description                                             |
| ------ | ------------------------------------------------------- |
| `0`    | The command ran successfully and no problem was found   |
| `1`    | The command ran successfully and some problem was found |
| `2`    | The command failed due to invalid command line option   |
| `3`    | The command failed due to some fatal error              |

<a id="on-github-actions"></a>

## Use actionlint on GitHub Actions

The JavaScript action runs the ordinary actionlint binary on Linux, macOS, and
Windows, including runners without Docker. Every invocation downloads the matching
actionlint binary and verifies its SHA-256 checksum. ShellCheck and pyflakes are
enabled by default; missing copies of these tools are installed in the runner's
tool cache. Python must be available when pyflakes is enabled. Problems appear as
GitHub annotations.

By default, `actionlint` and enabled `shellcheck`/`pyflakes` commands are available
on PATH in subsequent steps. Each tool has an independent PATH override.
For example, keep ShellCheck enabled for linting without adding it to PATH:

```yaml
- uses: kjanat/actionlint@v1
  with:
    add-shellcheck-to-path: false
- run: actionlint -version
```

Set `add-actionlint-to-path`, `add-shellcheck-to-path`, and `add-pyflakes-to-path`
to `false` to disable all PATH additions. Setting `shellcheck: false` or
`pyflakes: false` skips both provisioning and exporting that tool.

The exported actionlint binary stays in a fresh runner temporary directory until
job cleanup. Each invocation still downloads and verifies its own binary;
actionlint is never cached or reused from PATH.

```yaml
name: Lint GitHub Actions workflows
on: [push, pull_request]

jobs:
  actionlint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with: { persist-credentials: false }
      - name: Check workflow files
        uses: kjanat/actionlint@v1
```

`v1` follows compatible v1 releases, and minor tags follow patch releases. These
tags point to a release commit containing the JavaScript bundle. Pin that full
release commit SHA or an `action-vX.Y.Z` tag for an immutable JavaScript action reference.
`v1.17.0` is a versioned release tag for the CLI and source distribution.
New source tags and source checkouts do not contain generated JavaScript and
cannot be used directly as the action. Existing immutable release tags retain their
original implementation.

The action accepts these inputs:

| Input                        | Default       | Description                                                                   |
| ---------------------------- | ------------- | ----------------------------------------------------------------------------- |
| `files`                      | all workflows | Newline-separated workflow file paths                                         |
| `format`                     | `github`      | `github`, `default`, `oneline`, `json`, `json-lines`, `markdown`, or `sarif`  |
| `ignore`                     | none          | Newline-separated regular expressions for errors to ignore                    |
| `config-file`                | automatic     | Configuration file relative to `working-directory`                            |
| `config`                     | none          | Complete inline configuration as YAML or JSON                                 |
| `self-hosted-runner`         | inherited     | YAML/JSON mapping with a `labels` list                                        |
| `config-variables`           | inherited     | YAML/JSON list of permitted variable names, or `null`                         |
| `config-secrets`             | inherited     | YAML/JSON list of permitted secret names, or `null`                           |
| `paths`                      | inherited     | YAML/JSON mapping of workflow globs to configuration                          |
| `assume-default-permissions` | inherited     | `restricted` or `permissive`                                                  |
| `policy`                     | inherited     | YAML/JSON mapping of policy settings                                          |
| `shellcheck`                 | `true`        | Enable ShellCheck integration                                                 |
| `shellcheck-config`          | `false`       | `true` for discovery, `false` to disable, or a workspace-relative config file |
| `shellcheck-args`            | none          | Additional checking options as a YAML/JSON array of strings                   |
| `pyflakes`                   | `true`        | Enable pyflakes integration                                                   |
| `add-actionlint-to-path`     | `true`        | Make actionlint available on PATH for subsequent job steps                    |
| `add-shellcheck-to-path`     | `true`        | Make ShellCheck available on PATH when ShellCheck is enabled                  |
| `add-pyflakes-to-path`       | `true`        | Make pyflakes available on PATH when pyflakes is enabled                      |
| `working-directory`          | `.`           | Directory to lint, relative to the repository workspace                       |
| `output-file`                | none          | Repository-relative file to receive the selected output                       |
| `fail-on-error`              | `true`        | Fail when problems are found; command failures always fail                    |

When `config-file` is omitted, the action automatically loads
`.github/actionlint.yaml` or `.github/actionlint.yml` from the checked-out repository.
If both exist, `.yaml` wins. The log shows the selected file and any overriding
inputs. No config file is required.

ShellCheck configuration discovery is disabled by default, preserving existing
Action behavior for external rc files. For inline settings or an explicit rc path,
use [`tools.shellcheck.config` and `tools.shellcheck.enabled`](config.md#shellcheck)
in actionlint.yaml, or the corresponding `tools` Action input.

Set `shellcheck-config: true` to search for `.shellcheckrc` or `shellcheckrc`.
Discovery starts at the Action's `working-directory`, searches its parents, then checks
ShellCheck's user config locations. It does not start at each workflow file's
directory. An explicit config path is always relative to `GITHUB_WORKSPACE`,
even when `working-directory` points to a subdirectory; missing or unreadable
files are invalid inputs. This input retains its workspace-relative semantics;
paths inside `tools.shellcheck.config` instead default to the directory containing
actionlint.yaml. Relative source paths use each workflow step's effective working
directory; see [ShellCheck configuration](config.md#shellcheck).

```yaml
with:
  shellcheck-config: .github/shellcheckrc
  shellcheck-args: |
    - --severity=warning
    - --enable=check-unassigned-uppercase
    - --source-path
    - scripts with spaces
```

Arguments are passed literally, without shell expansion. Both `--option=value`
and separate value arguments work. Inherited `SHELLCHECK_OPTS` supplies
space-separated flags; `shellcheck-args` overrides single-value options such as
shell and severity. Include/exclude/enable lists and source paths accumulate,
with duplicate values removed. Use the array input for values containing spaces
or quotes. ShellCheck still interprets the selected configuration file itself.

`--rcfile` and `--norc` select configuration in argument order. An explicit
`shellcheck-config` input takes precedence over both; a blank input retains the
flag selection, then `tools.shellcheck.config`. Discovery stays disabled when none
selects a configuration.
Output flags are normalized to the internal JSON1 transport; use the Action's
`format` input to choose the report format. Help/version/list-only flags and
additional input files are rejected because they do not produce workflow
diagnostics. `--check-sourced` is also unsupported until findings in additional
files can be mapped to their own source locations. Both new inputs are ignored
when `shellcheck: false`.

Shell inference follows step, job and workflow defaults, then the runner:
Windows uses PowerShell, container jobs use `sh`, and other jobs assume Bash.
Known literal expressions are resolved; unresolved shell or container overrides
do not fall back to a guessed dialect. A runner lacking Bash may fall back to
`sh`; static analysis cannot detect its installed executables. Implicit Bash uses
`-e`; explicit `shell: bash` also enables `pipefail`. Custom templates only add
statically recognized startup options; quoted, escaped or dynamic arguments do
not receive guessed defaults. `--shell` overrides the analysis dialect for supported
shell scripts. See the [runner's shell handling](https://github.com/actions/runner/blob/main/src/Runner.Worker/Handlers/ScriptHandler.cs)
and [ShellCheck options](https://github.com/koalaman/shellcheck/blob/master/shellcheck.1.md#options).

All configuration inputs use the same names and types as the
[configuration file](config.md). Individual inputs override `config`, which
overrides the selected config file. Maps merge recursively; lists and scalars
replace the corresponding value. A blank input inherits, `{}` retains existing
map entries, `[]` clears a list, and `null` resets a value to its default.
Existing inputs, outputs, and failure behavior are retained.

For example, configure the custom runner directly:

```yaml
- uses: kjanat/actionlint@v1
  with:
    self-hosted-runner: |
      labels: [ubuntu-24.04-custom]
    policy: |
      require-job-timeout: true
```

`config` accepts both inline YAML and JSON, including output from `toJSON`:

```yaml
- uses: kjanat/actionlint@v1
  with:
    config: ${{ toJSON(fromJSON(vars.ACTIONLINT_CONFIG)) }}
```

Here `vars.ACTIONLINT_CONFIG` contains a JSON object. If it already contains
serialized YAML or JSON, `config: ${{ vars.ACTIONLINT_CONFIG }}` works directly.
An expression producing an object must use `toJSON` because action inputs are strings.
Unknown configuration keys and invalid input values produce an input error.

For `ignore`, write one regular expression per line. Inside a YAML block scalar
(`|`), quotes are literal characters, so do not wrap each pattern in quotes:

```yaml
- uses: kjanat/actionlint@v1
  with:
    ignore: |
      label "ubuntu-24\.04-custom" is unknown\.
```

For a single pattern, YAML scalar quoting is also valid:
`ignore: 'label "ubuntu-24\.04-custom" is unknown\.'`.
The action passes patterns directly to the regular expression engine; it does
not interpret shell quoting. `\.` matches a literal period; plain `.` matches
any character. If removing surrounding quotes would match a remaining problem,
the action adds a hint and keeps the original lint result.

The `exit-code`, `result`, `problems-found`, `problem-count`, `output`, and
`output-file` outputs can be used by later steps. For example, this writes JSON
Lines without failing the lint step, while still exposing whether problems were
found:

```yaml
- name: Check selected workflows
  id: actionlint
  uses: kjanat/actionlint@v1
  with:
    files: |
      .github/workflows/ci.yml
      .github/workflows/release.yml
    format: json-lines
    output-file: actionlint-results.jsonl
    fail-on-error: false
- name: Report result
  if: always()
  env:
    RESULT: ${{ steps.actionlint.outputs.result }}
    PROBLEM_COUNT: ${{ steps.actionlint.outputs.problem-count }}
  run: echo "$RESULT ($PROBLEM_COUNT problems)"
```

The download script remains useful when direct access to the executable is
preferred. It sets an absolute file path of the downloaded
executable to the `executable` output for following steps.

Here is an example of simple workflow to run actionlint on GitHub Actions.
Please ensure `shell: bash` since the default shell for Windows runners is
`pwsh`.

```yaml
name: Lint GitHub Actions workflows
on: [push, pull_request]

jobs:
  actionlint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with: { persist-credentials: false }
      - name: Download actionlint
        id: get_actionlint
        run: bash <(curl -fsSL https://raw.githubusercontent.com/kjanat/actionlint/662318dd6bbd9c0c120e35b03168bc1be69bf428/scripts/download-actionlint.bash) 1.17.0
        shell: bash
      - name: Check workflow files
        env: { actionlint: "${{ steps.get_actionlint.outputs.executable }}" }
        run: "${actionlint}" -color
        shell: bash
```

Or simply download the executable and run it in one step:

```yaml
- name: Check workflow files
  run: |
    bash <(curl -fsSL https://raw.githubusercontent.com/kjanat/actionlint/662318dd6bbd9c0c120e35b03168bc1be69bf428/scripts/download-actionlint.bash) 1.17.0
    ./actionlint -color
  shell: bash
```

The download script allows to specify the version of actionlint and the download
directory. Try to give `--help` argument to the script for more usage details.

If you want to enable
[shellcheck integration](checks.md#check-shellcheck-integ), install `shellcheck`
command. Note that shellcheck is
[pre-installed on Ubuntu worker][preinstall-ubuntu].

If you want to [annotate errors][ga-annotate-error] from actionlint on GitHub,
consider using [Problem Matchers](#problem-matchers).

If you prefer Docker image to running a downloaded executable, using
[actionlint Docker image](#docker) is another option.

```yaml
name: Lint GitHub Actions workflows
on: [push, pull_request]

jobs:
  actionlint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with: { persist-credentials: false }
      - name: Check workflow files
        uses: docker://ghcr.io/kjanat/actionlint:latest
        with:
          args: -color
```

## Online playground

Thanks to WebAssembly, actionlint playground is available on your browser. It
never sends any data to outside your browser.

<https://kjanat.github.io/actionlint>

Paste your workflow content to the code editor at left pane. It automatically
shows the results at right pane. When editing the workflow content in the code
editor, the results will be updated on the fly. Clicking an error message in the
results table moves a cursor to position of the error in the code editor.

<a id="docker"></a>

## [Docker][docker] image

[![Docker Image Version][docker-badge]][dockerhub]

[Docker image][docker-image] is available. The image contains `actionlint`
executable and all dependencies (shellcheck and pyflakes).

Available tags are:

- `ghcr.io/kjanat/actionlint:latest`:\
  Moving alias for the latest stable version of actionlint. This image is recommended.
- `ghcr.io/kjanat/actionlint:{version}`:\
  Release-specific actionlint image rather than a moving alias.\
  (e.g. `ghcr.io/kjanat/actionlint:1.17.0`)

The CLI image is also published to Docker Hub as `kjanat/actionlint:latest` and
`kjanat/actionlint:{version}`. Both registries carry the same manifest, so pick
whichever your setup pulls from more easily. Previous `action-*` images remain
available for older action refs; new releases use the JavaScript action.
The ordinary binary also supports `-github-action` to run the action adapter
with `INPUT_*`, `GITHUB_WORKSPACE`, and `GITHUB_OUTPUT` environment variables.

For byte-for-byte reproducibility, use the image's manifest digest as
`ghcr.io/kjanat/actionlint:{version}@sha256:<digest>`.

Just run the image with `docker run`:

```sh
docker run --rm ghcr.io/kjanat/actionlint:latest -version
```

To check all workflows in your repository, mount your repository at the image's
default working directory, `/w`:

```sh
docker run --rm -v "$(git rev-parse --show-toplevel):/w" ghcr.io/kjanat/actionlint:latest -color
```

To check a file with actionlint in a Docker container, pass the file content via
stdin and use `-` argument:

```sh
cat /path/to/workflow.yml | docker run --rm -i ghcr.io/kjanat/actionlint:latest -color -
```

Or mount the workflows directory and pass the paths as arguments:

```sh
docker run --rm -v /path/to/workflows:/workflows ghcr.io/kjanat/actionlint:latest -color /workflows/ci.yml
```

The container inherits its environment from `docker run`, so `SHELLCHECK_OPTS`
reaches shellcheck inside the image only when you pass it in with `-e`:

```sh
docker run --rm -v "$(git rev-parse --show-toplevel):/w" \
  -e SHELLCHECK_OPTS='-e SC2086' ghcr.io/kjanat/actionlint:latest -color
```

The `action-*` images are the exception. Their `shellcheck` and `pyflakes`
inputs are booleans that only switch the integrations on or off, so
`SHELLCHECK_OPTS` in the step's `env:` is the only way to configure shellcheck
there.

## Using actionlint from Go program

Go APIs are available. See [the Go API document](api.md) for more details.

<a id="tools-integ"></a>

## Tools integration

### reviewdog

> [!NOTE]
> [`reviewdog/action-actionlint` v1.76.0 installs this fork's v1.17.0](https://github.com/reviewdog/action-actionlint/blob/v1.76.0/scripts/install-actionlint.sh).
> Check the installer at your pinned action revision to identify the distribution and version it runs.

<!-- separator -->

> [!WARNING]
> The [v1.76.0 entrypoint](https://github.com/reviewdog/action-actionlint/blob/v1.76.0/entrypoint.sh) does not preserve actionlint's exit status through its reporting pipeline.
> An execution or configuration failure without parseable findings can therefore go unreported by reviewdog.
> A successful reviewdog step alone does not establish that analysis completed successfully.
> Tracked in reviewdog/action-actionlint#240.

[reviewdog][reviewdog] is an automated review tool for various code hosting
services. It officially [supports actionlint][reviewdog-actionlint]. You can
check errors from actionlint easily with inline review comments at pull request
review.

The usage is easy. Run `reviewdog/action-actionlint` action in your workflow as
follows.

```yaml
name: reviewdog
on: [pull_request]
jobs:
  actionlint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        with: { persist-credentials: false }
      - uses: reviewdog/action-actionlint@v1
```

<a id="problem-matchers"></a>

### Problem Matchers

> [!NOTE]
> The example below downloads this fork and installs this fork's problem matcher,
> so annotations include fork-specific checks.

[Problem Matchers][problem-matchers] is a feature to extract GitHub Actions
annotations from terminal outputs of linters.

Copy [actionlint-matcher.json][actionlint-matcher] to
`.github/actionlint-matcher.json` in your repository.

Then enable the matcher using `add-matcher` command before running `actionlint`
in the step of your workflow.

```yaml
- name: Check workflow files
  run: |
    echo "::add-matcher::.github/actionlint-matcher.json"
    bash <(curl -fsSL https://raw.githubusercontent.com/kjanat/actionlint/662318dd6bbd9c0c120e35b03168bc1be69bf428/scripts/download-actionlint.bash) 1.17.0
    ./actionlint -color
  shell: bash
```

When you change your workflow and the changed line causes a new error, CI will
annotate the diff with the extracted error message.

<img src="https://cdn.jsdelivr.net/gh/rhysd/ss@5530c2526b44ad28dc12f91a3d71bcd57940f008/actionlint/problem-matcher.png" alt="annotation by Problem Matchers" width="715" height="221" />

### super-linter

> [!WARNING]
> super-linter copies the binary from the `rhysd/actionlint` container image
> into its own image. Its GitHub Actions linter therefore runs upstream, not
> this fork.

[super-linter][super-linter] is a Bash script for a simple combination of
various linters, provided by GitHub. It has support for actionlint. Running
super-linter in your repository automatically runs actionlint.

To ignore some errors, please add `-ignore` option by using
[`GITHUB_ACTIONS_COMMAND_ARGS` environment variable][super-linter-env-var].
Please see
super-linter/super-linter#1852
for the discussion.

### pre-commit

> [!NOTE]
> The configuration below points directly at `kjanat/actionlint`. The
> `actionlint` hook builds this fork, `actionlint-docker` pulls this fork's
> image, and `actionlint-system` runs the `actionlint` executable on `PATH`.
> `actionlint-shellcheck` builds this fork and installs ShellCheck next to it.

[pre-commit][pre-commit] is a framework for managing and maintaining
multi-language Git pre-commit hooks. actionlint is available as a pre-commit
hook to check workflow files in `.github/workflows/` directory.

Add this to your `.pre-commit-config.yaml` in your repository:

```yaml
---
repos:
  - repo: https://github.com/kjanat/actionlint
    rev: v1.17.0
    hooks:
      - id: actionlint
```

As alternatives to `actionlint` hook, `actionlint-docker`, `actionlint-system`,
or `actionlint-shellcheck` hooks are available.

| Hook ID                 | Explanation                                                                                                                                                                                                                         |
| ----------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `actionlint`            | Automatically installs `actionlint` command in isolated `$GOPATH` directory using [Go toolchain][go-install].                                                                                                                       |
| `actionlint-docker`     | Automatically pulls [the actionlint Docker image](#docker).                                                                                                                                                                         |
| `actionlint-system`     | Uses system-installed `actionlint` command. The command is necessary to be [installed manually](install.md).                                                                                                                        |
| `actionlint-shellcheck` | Same as `actionlint`, and additionally installs a Go build of ShellCheck ([`wasilibs/go-shellcheck`][go-shellcheck]) so [the shellcheck integration](checks.md#check-shellcheck-integ) works without a host-installed `shellcheck`. |

The `actionlint` hook installs into an isolated `$GOPATH`, so it only finds a
`shellcheck` executable that is already on `PATH`.

`actionlint-shellcheck` pins go-shellcheck so each actionlint revision builds a
reproducible pre-commit environment. The scheduled [Upkeep workflow](../.github/workflows/upkeep.yml) checks both go-shellcheck
and the ShellCheck version it embeds, and proposes pin updates automatically.
To choose a different version yourself, use `additional_dependencies` on the
plain hook:

```yaml
---
repos:
  - repo: https://github.com/kjanat/actionlint
    rev: v1.17.0
    hooks:
      - id: actionlint
        additional_dependencies:
          - github.com/wasilibs/go-shellcheck/cmd/shellcheck@v0.11.1
```

### VS Code

> [!NOTE]
> The extension runs the command configured in `linter-actionlint.config`,
> which defaults to `actionlint` on `PATH`. It uses this fork when that command
> resolves to a fork build; it does not download actionlint itself.

[Linter extension][vsc-extension] for [VS Code][vscode] is available. The
extension automatically detects `.github/workflows` directory, runs `actionlint`
command, and reports errors in the code editor while editing workflow files.

### Emacs

> [!NOTE]
> Both plugins run a locally installed executable named `actionlint` by
> default. Flycheck exposes `flycheck-actionlint-executable`, and Flymake
> exposes `flymake-actionlint-executable`, so either can select this fork
> explicitly.

Plugins for both [Flycheck][emacs-flycheck] and [Flymake][emacs-flymake] are
available via [MELPA][emacs-melpa].

Their respective repositories are
[flycheck-actionlint][emacs-flycheck-extension] and
[flymake-actionlint][emacs-flymake-extension].

### Vim and Neovim

> [!NOTE]
> Both integrations run a local `actionlint` executable. nvim-lint's linter
> command can be overridden, and ALE exposes
> `g:ale_yaml_actionlint_executable`, so either can select this fork.

[nvim-lint][nvim-lint] supports actionlint on Neovim. The plugin automatically
and asynchronously runs actionlint and notifies errors on the fly when you edit
GitHub Actions CI workflows. Please read the plugin's documentation for more
details.

[ALE][vim-ale] supports actionlint on Vim and Neovim. Similar to nvim-lint, The
plugin automatically and asynchronously runs actionlint and notifies errors on
the fly when you edit GitHub Actions CI workflows. Please read the plugin's
documentation for more details.

### Pulsar Edit

> [!NOTE]
> The package runs the local executable configured by `actionsExecutablePath`,
> which defaults to `actionlint` on `PATH`. Point that setting at this fork's
> binary to use fork-specific checks.

A [Linter package][pulsar-linter] for [Pulsar Edit][pulsar] is available. The
package automatically detects a `workflows` directory, executes the `actionlint`
command on any detected GitHub Actions files within the directory, and reports
returned information in the code editor display tab while editing workflow
files.

### Nova

> [!NOTE]
> The extension runs the local executable configured by
> `actionlint.binarypath`, which defaults to `actionlint` on `PATH`. Point that
> setting at this fork's binary to use fork-specific checks.

[Nova.app][nova] is a MacOS only editor and IDE. The
[Actionlint for Nova][nova-extension] allows you to get inline feedback while
editing actions.

### trunk

> [!WARNING]
> trunk's actionlint plugin downloads releases from `rhysd/actionlint` using
> hard-coded upstream URLs. The `trunk check enable actionlint` commands below
> install upstream, not this fork.

[trunk][trunk-io] is an extendable superlinter with a builtin language server
and preexisting issue detection. Actionlint is integrated in [trunk-io/plugins].

Once you have
[initialized trunk in your repo](https://docs.trunk.io/docs/check-get-started),
to enable at the latest actionlint version, just run:

```bash
trunk check enable actionlint
```

or if you'd like a specific version:

```bash
trunk check enable actionlint@1.17.0
```

or modify `.trunk/trunk.yaml` in your repository to contain:

```yaml
lint:
  enabled:
    - actionlint@1.17.0
```

Then just run:

```bash
trunk check
```

and it will check your modified files via actionlint, if applicable, and show
you the results. Trunk also will detect preexisting issues and highlight only
the newly added actionlint issues. For more information, check the
[trunk docs][trunk-docs].

You can also see actionlint issues inline in VS Code via the [Trunk VS Code extension][trunk-vscode].

---

[Checks](checks.md) | [Installation](install.md) | [Configuration](config.md) | [Go API](api.md) | [References](reference.md)

[actionlint-matcher]: https://raw.githubusercontent.com/kjanat/actionlint/HEAD/.github/actionlint-matcher.json
[cmd-manual]: https://kjanat.github.io/actionlint/usage.html
[docker-badge]: https://img.shields.io/docker/v/kjanat/actionlint
[dockerhub]: https://hub.docker.com/r/kjanat/actionlint
[docker-image]: https://github.com/kjanat/actionlint/pkgs/container/actionlint
[docker]: https://www.docker.com/
[emacs-flycheck-extension]: https://github.com/tirimia/flycheck-actionlint
[emacs-flycheck]: https://www.flycheck.org/
[emacs-flymake-extension]: https://github.com/ROCKTAKEY/flymake-actionlint
[emacs-flymake]: https://www.gnu.org/software/emacs/manual/html_node/flymake/
[emacs-melpa]: https://melpa.org/
[ga-annotate-error]: https://docs.github.com/en/actions/learn-github-actions/workflow-commands-for-github-actions#setting-an-error-message
[go-install]: https://go.dev/doc/install
[go-shellcheck]: https://github.com/wasilibs/go-shellcheck
[go-template]: https://pkg.go.dev/text/template
[jsonl]: https://jsonlines.org/
[nova-extension]: https://extensions.panic.com/extensions/org.netwrk/org.netwrk.actionlint/
[nova]: https://nova.app
[nvim-lint]: https://github.com/mfussenegger/nvim-lint
[pre-commit]: https://pre-commit.com
[preinstall-ubuntu]: https://github.com/actions/runner-images/blob/main/images/ubuntu/Ubuntu2404-Readme.md
[problem-matchers]: https://github.com/actions/toolkit/blob/master/docs/problem-matchers.md
[pulsar-linter]: https://web.pulsar-edit.dev/packages/linter-github-actions
[pulsar]: https://pulsar-edit.dev/
[re2]: https://golang.org/s/re2syntax
[release-badge]: https://img.shields.io/github/v/release/kjanat/actionlint
[releases]: https://github.com/kjanat/actionlint/releases
[reviewdog-actionlint]: https://github.com/reviewdog/action-actionlint
[reviewdog]: https://github.com/reviewdog/reviewdog
[sarif]: https://docs.oasis-open.org/sarif/sarif/v2.1.0/sarif-v2.1.0.html
[super-linter-env-var]: https://github.com/super-linter/super-linter#environment-variables
[super-linter]: https://github.com/github/super-linter
[trunk-docs]: https://docs.trunk.io/docs/check
[trunk-io]: https://docs.trunk.io/docs
[trunk-io/plugins]: https://github.com/trunk-io/plugins
[trunk-vscode]: https://marketplace.visualstudio.com/items?itemName=trunk.io
[vim-ale]: https://github.com/dense-analysis/ale
[vsc-extension]: https://marketplace.visualstudio.com/items?itemName=arahata.linter-actionlint
[vscode]: https://code.visualstudio.com/
