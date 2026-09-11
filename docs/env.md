# Environment variables

The CLI reads the variables below as defaults. Explicit flags take precedence,
including `false`, empty values, and legacy spellings such as `-config-file`.
Settings apply to both root invocations and the `check` command. Go library calls
do not read `ACTIONLINT_*` variables.

Only settings used by the selected operation are read. Help, version, rules and
completion do not load configuration or initialize external linters. `config init`
always creates the repository config and ignores environment config selection.

Boolean values accept `true`/`false`, `1`/`0`, `t`/`f`, and their Go-supported case
variants. An empty boolean value means false. Empty enum values use the default.
Invalid relevant environment settings produce an invocation error (exit 2).
Invalid configuration file contents still produce an operational error (exit 3).

## Configuration and input

| Variable                    | Equivalent flag               | Value                                                                                                       |
| --------------------------- | ----------------------------- | ----------------------------------------------------------------------------------------------------------- |
| `ACTIONLINT_CONFIG`         | `--config` / `--config-file`  | A configuration file path. Empty restores repository discovery.                                             |
| `ACTIONLINT_NO_CONFIG`      | `--no-config`                 | Skip configuration loading. Default: false.                                                                 |
| `ACTIONLINT_CONFIG_ORIGIN`  | `config show --origin`        | Include the source of each config setting. Default: false.                                                  |
| `ACTIONLINT_STDIN_FILENAME` | `--stdin-filename`            | Filename for stdin diagnostics and project detection. Default: `<stdin>`.                                   |
| `ACTIONLINT_IGNORE_REGEX`   | `--ignore-regex` / `--ignore` | JSON array of message-filter regexes, such as `["unknown label", "SC2086"]`. Empty or `[]` adds no filters. |

Any explicit config-selection flag overrides both `ACTIONLINT_CONFIG` and
`ACTIONLINT_NO_CONFIG`. Without flags, setting a nonempty config path alongside
`ACTIONLINT_NO_CONFIG=true` is an error. Relative paths use the current directory.
Environment selection works for checks, `doctor`, and config inspection commands.

There is still one configuration file, without a global search or merging between
files. Policy settings, runner labels, allowed secrets and variables remain in
that file; see [Configuration](config.md). `config show --origin` reports the
origins of values inside the selected file.

Explicit `--ignore` or `--ignore-regex` options replace the environment's entire
filter list. Repeated explicit options still accumulate as before. These filters
match diagnostic messages.

```powershell
$env:ACTIONLINT_CONFIG = 'C:\Projects\shared-config\actionlint.yaml'
$env:ACTIONLINT_IGNORE_REGEX = '["SC2086"]'
actionlint check workflow.yml
actionlint config show --origin
```

## Output and logging

| Variable                   | Equivalent flag                  | Value                                                                                 |
| -------------------------- | -------------------------------- | ------------------------------------------------------------------------------------- |
| `ACTIONLINT_OUTPUT_FORMAT` | `--output-format` / `-o`         | `text`, `oneline`, `json`, `jsonl`, `sarif`, or `github`. Checks only; default: text. |
| `ACTIONLINT_JSON`          | `--json`                         | JSON diagnostics or command metadata. Default: false.                                 |
| `ACTIONLINT_JSON_PRETTY`   | `--json-pretty`                  | Use installed `jq` for terminal JSON. Default: true.                                  |
| `ACTIONLINT_TEMPLATE`      | `--template` / `--format` / `-f` | A legacy Go template. Checks only.                                                    |
| `ACTIONLINT_TEMPLATE_FILE` | `--template-file`                | A file containing a Go template. Checks only.                                         |
| `ACTIONLINT_OUTPUT_FILE`   | `--output-file`                  | Report destination. Empty or `-` writes to stdout. Checks only.                       |
| `ACTIONLINT_LOG_LEVEL`     | `--log-level`                    | `none`, `info`, or `debug`. Checks only; default: none.                               |
| `ACTIONLINT_QUIET`         | `--quiet` / `-q`                 | Suppress progress and summaries, retaining findings and errors. Default: false.       |
| `ACTIONLINT_SUMMARY`       | `--summary`                      | Write a check summary to stderr. Default: false.                                      |
| `ACTIONLINT_COLOR`         | Color controls                   | `auto`, `always`, or `never`. Default: auto.                                          |
| `ACTIONLINT_HYPERLINKS`    | `--hyperlinks`                   | `auto`, `always`, or `never`. Default: auto.                                          |

An explicit format, JSON, template, template-file or oneline flag replaces the
environment's whole format selection. Conflicting environment selections are
rejected, just like conflicting flags. Format and output destination are separate;
use `--output-file=-` to override an environment report path.

Explicit `--log-level`, `--verbose`/`-v`, or `--debug` overrides the environment log
level. `--quiet=false` and `--summary=false` can disable environment defaults.

### Terminal JSON

When stdout is a TTY and `jq` is on `PATH`, JSON metadata, JSON diagnostics and
SARIF reports use `jq`'s identity filter (`.`) for indentation and terminal color.
This includes `--version --json`, `version --json`, `doctor --json`, config/rules/help
JSON, and `check --json`.

Pipes and regular files retain their existing bytes. JSONL, legacy templates and
stderr log/error records are never passed to `jq`. If `jq` is absent, fails, or
takes more than two seconds, actionlint writes its original JSON. Nothing is
installed automatically. Cancellation still cancels the command.

```sh
actionlint --version --json
actionlint doctor --json
actionlint doctor --json --json-pretty=false
actionlint check --json workflow.yml > findings.json
```

### Color and hyperlinks

- [NO_COLOR](https://no-color.org/): a nonempty `NO_COLOR` disables automatic color
  and overrides `ACTIONLINT_COLOR`. Empty has no effect. Explicit CLI color controls
  take precedence; the root's `--no-color` still wins when both booleans are true.
- [NO_HYPERLINKS](https://no-hyperlinks.org/spec): a nonempty `NO_HYPERLINKS` disables
  links and overrides `ACTIONLINT_HYPERLINKS`. Otherwise, that variable selects the
  mode. In `auto`, nonempty `FORCE_HYPERLINKS` enables links before terminal detection.
  Explicit `--hyperlinks` takes precedence over these environment variables.
- A value of `0` or `false` is nonempty for these convention variables.
- `GITHUB_ACTIONS=true` enables automatic color in help and text diagnostic logs
  without requiring a TTY. It does not enable hyperlinks or JSON pretty-printing.
- `TERM=dumb` disables automatic terminal styling outside GitHub Actions.
  Hyperlink detection also uses terminal identity/version hints, including
  `TERM_PROGRAM`, `TERM_PROGRAM_VERSION`, `WT_SESSION`, and `VTE_VERSION`.
  Multiplexer hints (`TMUX`, `STY`, and matching `TERM` values) disable automatic links.

Color and hyperlinks are independent. Links appear in help and doctor paths;
structured output never receives OSC 8 links. Optional terminal JSON color follows
the same color selection. `JQ_COLORS` is interpreted by `jq` itself.

## External linters

| Variable                      | Meaning                                                                               |
| ----------------------------- | ------------------------------------------------------------------------------------- |
| `ACTIONLINT_SHELLCHECK_BIN`   | Literal ShellCheck executable name or path. Default: `shellcheck`. Empty disables it. |
| `ACTIONLINT_SHELLCHECK_FLAGS` | Additional ShellCheck arguments.                                                      |
| `ACTIONLINT_SHELLCHECK_ENV`   | Child environment overrides or forwarded variable names for ShellCheck.               |
| `ACTIONLINT_PYFLAKES_BIN`     | Literal Pyflakes executable name or path. Default: `pyflakes`. Empty disables it.     |
| `ACTIONLINT_PYFLAKES_FLAGS`   | Additional Pyflakes arguments, for example `-m pyflakes` when the binary is Python.   |
| `ACTIONLINT_PYFLAKES_ENV`     | Child environment overrides or forwarded variable names for Pyflakes.                 |

`_BIN` is a literal executable, so Windows paths containing spaces need no embedded
quotes. `_FLAGS` accepts a JSON string array or a shell-style quoted argument list.
JSON is useful for empty arguments and paths containing backslashes. There is no
shell execution, environment expansion, command substitution, piping or redirection.

```powershell
$env:ACTIONLINT_SHELLCHECK_BIN = 'C:\Program Files\ShellCheck\shellcheck.exe'
$env:ACTIONLINT_SHELLCHECK_FLAGS = '["-e", "SC2086"]'
$env:ACTIONLINT_PYFLAKES_BIN = 'python3'
$env:ACTIONLINT_PYFLAKES_FLAGS = '-m pyflakes'
actionlint check workflow.yml
```

An explicit `--shellcheck` or `--pyflakes` replaces **all three** environment
settings for that tool. The flags retain their existing command-line syntax;
`--shellcheck=` disables ShellCheck even when its environment settings are present.
An empty `_BIN` also disables its tool without parsing its `_FLAGS` or `_ENV`.

Extra arguments precede actionlint's required arguments. Do not supply input files
or override the output protocol: ShellCheck must return JSON1. Existing
`SHELLCHECK_OPTS` behavior remains available; ShellCheck interprets that variable.

`_ENV` accepts a JSON object with string values, or a quoted list of `NAME=value`
entries and variable names. JSON `null` means forward the current value. A bare
name also forwards its current value; absent names stay absent. Empty strings set
empty values. Names use `[A-Za-z_][A-Za-z0-9_]*`.

```powershell
$env:ACTIONLINT_SHELLCHECK_ENV = '{"LANG":"C.UTF-8","SHELLCHECK_OPTS":null}'
# Equivalent argument-list form:
$env:ACTIONLINT_SHELLCHECK_ENV = 'LANG=C.UTF-8 SHELLCHECK_OPTS'
```

Child processes inherit the full parent environment, with these entries overriding it.
Overrides apply only to the selected tool;
they do not modify actionlint's environment or the other linter. `doctor --json`
reports the resolved executable and arguments without executing the tools or
printing environment override values.

## Other environment variables

`PATH` locates tools and optional `jq`; Windows also uses `PATHEXT`. Completion
shell detection uses `SHELL` and the PowerShell fallback `PSModulePath`. See
[Usage](usage.md) for command behavior and [Configuration](config.md) for policy settings.
