# Environment variables

Explicit flags win, including `false` and empty values.

An empty boolean means false. An invalid value exits with status 2.

## Configuration and input

| Variable                    | Equivalent flag        | Value                                                                           |
| --------------------------- | ---------------------- | ------------------------------------------------------------------------------- |
| `ACTIONLINT_CONFIG`         | `--config`             | Configuration file path. Empty restores repository discovery.                   |
| `ACTIONLINT_NO_CONFIG`      | `--no-config`          | Skip configuration loading.                                                     |
| `ACTIONLINT_CONFIG_ORIGIN`  | `config show --origin` | Show the source of each setting.                                                |
| `ACTIONLINT_STDIN_FILENAME` | `--stdin-filename`     | Filename for stdin diagnostics. Default: `<stdin>`.                             |
| `ACTIONLINT_IGNORE_REGEX`   | `--ignore-regex`       | JSON array of message regexes, such as `["SC2086"]`.                            |
| `ACTIONLINT_STRICT`         | `--strict`             | Enable stable `correctness`, `security` and `suspicious` rules.                 |
| `ACTIONLINT_EXPERIMENTAL`   | `--experimental`       | Enable nursery rules. False disables them, including individually enabled ones. |

A config flag overrides both config variables. A nonempty `ACTIONLINT_CONFIG` together with
`ACTIONLINT_NO_CONFIG=true` is an error.

```powershell
$env:ACTIONLINT_CONFIG = 'C:\Projects\shared-config\actionlint.yaml'
$env:ACTIONLINT_IGNORE_REGEX = '["SC2086"]'
actionlint check workflow.yml
actionlint config show --origin
```

## Output and logging

| Variable                   | Equivalent flag   | Value                                                                                      |
| -------------------------- | ----------------- | ------------------------------------------------------------------------------------------ |
| `ACTIONLINT_OUTPUT_FORMAT` | `--output-format` | `text`, `pretty`, `oneline`, `gcc`, `json`, `jsonl`, `sarif` or `github`. Default: pretty. |
| `ACTIONLINT_JSON`          | `--json`          | JSON output.                                                                               |
| `ACTIONLINT_JSON_PRETTY`   | `--json-pretty`   | Format terminal JSON with `jq`. Default: true.                                             |
| `ACTIONLINT_TEMPLATE`      | `--template`      | Go template.                                                                               |
| `ACTIONLINT_TEMPLATE_FILE` | `--template-file` | Go template file.                                                                          |
| `ACTIONLINT_OUTPUT_FILE`   | `--output-file`   | Report path. Empty or `-` means stdout.                                                    |
| `ACTIONLINT_LOG_LEVEL`     | `--log-level`     | `none`, `info` or `debug`.                                                                 |
| `ACTIONLINT_QUIET`         | `--quiet`         | Suppress progress and summaries.                                                           |
| `ACTIONLINT_SUMMARY`       | `--summary`       | Show the summary. Default: true.                                                           |
| `ACTIONLINT_COLOR`         | `--color`         | `auto`, `always` or `never`.                                                               |
| `ACTIONLINT_HYPERLINKS`    | `--hyperlinks`    | `auto`, `always` or `never`.                                                               |

Any explicit format flag replaces the environment's format selection. Use `--output-file=-` to
override `ACTIONLINT_OUTPUT_FILE`. When that file exists, actionlint asks before it overwrites the
file. Noninteractive runs and checks of stdin need `--yes`.

```sh
actionlint --version --json
actionlint doctor --json
actionlint doctor --json --json-pretty=false
actionlint check --json workflow.yml >findings.json
```

### Color and hyperlinks

- A nonempty [NO_COLOR](https://no-color.org/) disables automatic color. A nonempty
  [NO_HYPERLINKS](https://no-hyperlinks.org/spec) disables links. `0` and `false` count as
  nonempty.
- A nonempty `FORCE_HYPERLINKS` enables links in `auto` mode.
- Terminal multiplexers disable automatic links.

## External linters

Each bundled tool reads three variables. Uppercase the tool ID and replace dashes with
underscores.

| Variable                     | Meaning                                                                                       |
| ---------------------------- | --------------------------------------------------------------------------------------------- |
| `ACTIONLINT_TOOL_<ID>_BIN`   | Executable name or path. Default: the tool ID, such as `shellcheck`. Empty disables the tool. |
| `ACTIONLINT_TOOL_<ID>_FLAGS` | Extra arguments, as a JSON string array or a shell-style quoted list.                         |
| `ACTIONLINT_TOOL_<ID>_ENV`   | Child environment overrides.                                                                  |

actionlint runs no shell and expands no variables. Extra arguments precede actionlint's own. Do
not pass input files or change the output format.

```powershell
$env:ACTIONLINT_TOOL_SHELLCHECK_BIN = 'C:\Program Files\ShellCheck\shellcheck.exe'
$env:ACTIONLINT_TOOL_SHELLCHECK_FLAGS = '["-e", "SC2086"]'
actionlint check workflow.yml
```

`--tool ID` and `--no-tool ID` replace all three variables for that tool.

`_ENV` accepts a JSON object or a quoted list of `NAME=value` entries and bare names. JSON
`null` or a bare name forwards the current value.

```powershell
$env:ACTIONLINT_TOOL_SHELLCHECK_ENV = '{"LANG":"C.UTF-8","SHELLCHECK_OPTS":null}'
$env:ACTIONLINT_TOOL_SHELLCHECK_ENV = 'LANG=C.UTF-8 SHELLCHECK_OPTS'
```

`doctor --json` shows the resolved executable and arguments.

## Other environment variables

`PATH` locates tools and `jq`.
