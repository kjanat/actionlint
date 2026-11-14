# Usage

[![GitHub Release][release-badge]][releases]

This document describes how to use [actionlint](../README.md).

## `actionlint` command

With no arguments inside a Git repository, `actionlint` checks workflows and
`action.yml`/`action.yaml` manifests in that repository. Outside a repository,
it prints help to stdout and exits 0. `actionlint check` explicitly requests
repository discovery and reports an error outside a repository.

Discovery includes actions that no workflow references. Repositories containing
only actions are supported too. If discovery finds no inputs, the check exits 3.

```sh
actionlint check
```

When paths to YAML workflow files are given as arguments, actionlint checks them.

```sh
actionlint path/to/workflow1.yaml path/to/workflow2.yaml
```

Directories are search roots: actionlint finds `action.yml`/`action.yaml` manifests
and YAML files below `.github/workflows`, including in nested repositories.
Each repository uses its own Git ignore rules, configuration and plugins.
Other YAML files are not selected by directory discovery. Overlapping paths and
aliases check each physical file once. An empty selection reports an error.

Directories and glob matches respect configured file filters and Git ignore rules.
A literally named file overrides both, even outside the conventional locations.
Use `--no-ignore` to scan Git-ignored workflows and actions or files excluded by
your actionlint configuration.

Quote globs to let actionlint expand `*`, `?` and `[]` consistently across shells.
A leading `~/` expands to the home directory.

```sh
actionlint .
actionlint .github/
actionlint check .github/workflows/ .github/actions/
actionlint '~/projects/*'
actionlint ~/projects --no-ignore
```

Use `-C DIR` to run from another directory, including configuration discovery and
relative input/output paths. Repeated relative `-C` values build on the previous one:

```sh
actionlint -C path/to/repository .github/
actionlint -C path/to/repository -C testdata .
```

Explicit `action.yml` and `action.yaml` inputs are checked as action metadata,
including composite steps and their ShellCheck and Ruff findings. You can mix
workflows and actions in one invocation:

```sh
actionlint .github/workflows/ci.yml .github/actions/build/action.yml
```

When a selected workflow references a selected action, both use the supplied
action content. Shared metadata defects are reported once at their declaration;
caller-specific input errors still point to the workflow's `uses:` step.

Files directly under `.github/workflows` are parsed as workflows regardless of filename.

Metadata findings point into the action file. Configuration overrides, rule levels
and ignores apply to that file. Repository discovery without paths stays in that
repository; directory searches also enter nested repositories. Directory symlinks
are not followed unless explicitly named, and discovered action file symlinks are skipped.
Inside a Git repository, discovery
asks `git` 2.8 or newer which paths its ignore rules exclude: `.gitignore`,
`.git/info/exclude` and the global excludes file apply, and tracked manifests in
ignored directories stay discovered. Discovery fails when `git` is missing or
reports an incomplete listing.
Use [file exclusions](config.md) to
omit fixtures or generated actions. Literally named files bypass discovery filters.

Standalone actions have no known runner, so `executable-bit` is checked only when
an action is reached through a workflow with a known runner.

Pass `-` to read workflow source from stdin.

```sh
cat path/to/workflow.yaml | actionlint -
```

Use `actionlint --help` for common options and commands, and
`actionlint check --help` for all checking options. The root invocation also
accepts the options listed under `check`. See [the online command manual][cmd-manual]
for the full reference.

### Commands and compatibility

The root invocation and the explicit `check` command accept options before or
after filenames. Legacy single-dash names such as `-format` are accepted at the
root and after subcommands, alongside `--format`. Boolean
options accept `=false`. Both routes use the same analysis engine. Use `--` to
treat every remaining argument as a filename, including names beginning with `-`.

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

`check` accepts workflow files, action manifests, directories or a lone `-` for stdin. With no
paths, it uses repository discovery. Use `--stdin-filename=action.yml` to check action
metadata from stdin.

A first argument named `check`, `config`, `rules`, `doctor`, `completion`,
`version` or `help` selects that command. To check a file with one of those names,
pass it as `./version`, after `--`, or to `check`.
Root options still belong before filenames:

```sh
actionlint -- --workflow.yml
actionlint ./version
actionlint check version --json
```

Normal help and completion recommend the modern names. `--help-legacy` lists the
supported old names without deprecation warnings. `--format` selects a Go template.
`-o`, `-f`, `-q` and `-h` mean output format, template, quiet and help.
Use `--log-level=info` or legacy `-v` for progress logging.
`--version` (or `-V`) prints actionlint's version and build information. It also
works after `check`, `fix` and `pre-commit`, including through wrappers that add
one of those commands.
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

Pretty diagnostics make HTTP and HTTPS URLs clickable using OSC 8 terminal
hyperlinks. When a URL wraps across lines, each part opens the full URL.
Help also links the project name and documentation URLs. Its documentation URL uses the release tag or the build's
source commit. Without embedded version or VCS metadata, it falls back to `HEAD`;
use `go run -buildvcs=true ./cmd/actionlint --help` to include the checkout revision
when running from source.

Doctor uses `file://` links for its directory, configuration
file and resolved executables. Select `--hyperlinks=auto|always|never` independently of
color. The default `auto` follows the [no-hyperlinks convention](https://no-hyperlinks.org/spec):
non-empty `NO_HYPERLINKS` disables links, then non-empty `FORCE_HYPERLINKS`
enables them, then the output stream's TTY status and known terminal capabilities decide:
stderr for help, stdout for doctor and diagnostic output for pretty reports.
Even `0` is a non-empty value. Explicit `always` or `never` overrides both variables.
Unknown terminals and multiplexers use plain URLs in auto mode; `always` can
enable links when the terminal and multiplexer are configured to pass them through.
Both modes show destination URLs.

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
actionlint check --output-format=gcc
actionlint check --template-file=report.tmpl
actionlint check --help --json
actionlint version --json
```

`--output-format` accepts `text`, `pretty`, `oneline`, `gcc`, `json`, `jsonl`, `sarif` and `github`.
`pretty` is the default. Repeat `--output-format`, `--output` or `-o` to render
the same analysis in multiple formats, in order. For example,
`actionlint --output github --output pretty` emits annotations followed by terminal
diagnostics. Repeated identical formats are emitted once.

Set `output.formats`, `output.log-level`, `output.summary`, `output.network-stats`
and `output.quiet` in the configuration file to use them as defaults for checks.
An explicit `--config` selects those defaults. Otherwise actionlint uses the
configuration in the repository containing the current directory, including
the directory selected with `-C`. That choice applies to the entire command,
even when it scans several repositories. Each scanned repository still supplies
its own lint rules. CLI flags override environment settings, which override
configured defaults. `--no-config` disables both reporting and lint configuration.
`gcc` emits one uncoloured `file:line:column: severity: message [code]` line per
finding, using its native diagnostic code or rule name. Information is emitted
as `note`; multiline messages are flattened without source excerpts.
`--json` selects JSON. `--template` and its supported `--format` alias render the
existing Go-template fields and functions. `--template-file` reads the same syntax
from a file. A template cannot be combined with a built-in output selector.
The legacy `-oneline` option still yields to a nonempty template.

For compact terminal diagnostics with severity colours, separate locations and
wrapped messages:

```sh
actionlint check --output-format=pretty --diagnostic-width=80 --color=auto
```

`pretty` includes captured source, range indicators, diagnostic-specific advice,
related locations and fix descriptions. Native diagnostic codes appear beside
the rule name. The `config` line shows where to configure the rule, for example
`lint.rules.suspicious.case-insensitive-conditions`. Use `actionlint rules RULE`
for the rule's explanation and options.
Messages and paths
wrap; long source lines show a window around the diagnostic. Tabs use four-column
stops. Width defaults to 100 columns; `--diagnostic-width` accepts 40 to 400 and
requires `pretty`. Use `--color=always` to force ANSI colours or `--color=never`
to disable them. Automatic colour respects `NO_COLOR`.

```text
warning[expression-reference]
  unknown variable
  --> ci.yml:1:3
  |
1 |   bad
  |   ^^^
  = help: Use a declared value.
```

JSON returns a versioned document. Each diagnostic has `rule`, `message`, `path`,
`start`, `end` and an optional `snippet`. Positions use one-based Unicode character
columns, and the end position is exclusive. Ranges may span multiple lines. For example:

```json
{
  "schema_version": 1,
  "status": "problems-found",
  "completed": true,
  "exit_code": 1,
  "file_count": 1,
  "configurations": [],
  "hints": [],
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

CLI and Action share [one result contract](results.md), published as a JSON Schema
and TypeScript types in the npm package. A clean JSON result has an empty `diagnostics` array. JSON Lines emits those
individual diagnostic objects with `schema_version: 1` on every record, or nothing for a clean result. Legacy
`-format '{{json .}}'` emits the legacy diagnostic array with fields including
`kind`, `filepath` and `end_column`. SARIF uses the bundled renderer. `github`
emits escaped workflow annotation commands; it is enabled only by that explicit
format choice, including when running inside GitHub Actions.

Check results go to stdout, or to `--output-file PATH`. `--output-file -` means stdout.
If the destination exists, an interactive terminal asks `Overwrite? [y/N]`
before analysis. Enter or a negative answer keeps the existing file and stops the
check. Use `-y` or `--yes` to answer yes without prompting. This is required when
stderr is not a terminal or `-` supplies workflow input; confirmation never consumes
workflow stdin. `yes | actionlint --output-file report.json` also works in an
interactive terminal. The destination option applies only to checks.
A report file is replaced only after analysis completes; an operational failure
preserves any previous report. An output file cannot replace a consumed workflow, local action, reusable workflow, configuration, template, or existing stdin filename, including links to those files. Replacing a report preserves its permission bits; a new report is private to the current user.
Logs and operational errors go to stderr. Structured output never includes ANSI
color or terminal hyperlinks. Checks print a summary to stderr by default.

Try additional checks without changing a configuration file:

```sh
actionlint --strict
actionlint --experimental
actionlint check --strict --experimental
```

`--strict` enables stable `correctness`, `security` and `suspicious` rules,
including non-recommended checks. It does not enable additional `conventions`
rules. Select those through configuration, individually or with `preset: all`.
Strict mode respects explicit group and rule exceptions,
timeout bounds, permission scopes, suppression report modes and rule-ID/path
ignores. It does not install missing external linters. `--experimental`
enables nursery rules except those explicitly excluded. Strict
mode alone does not enable nursery rules. Both switches apply after file
overrides; `--experimental=false` clears only nursery selections. The `join-literal` rule is in nursery.
`--strict=false` removes the strict preset; configure policies separately to disable them.
Use `ACTIONLINT_STRICT=true` and `ACTIONLINT_EXPERIMENTAL=true` for equivalent
environment defaults; explicit flags, including `false`, take precedence.
Set `lint.experimental: true` to enable nursery rules in a repository's
configuration. `ACTIONLINT_EXPERIMENTAL` and explicit `--experimental` flags
override that setting, including `false`.

`--log-level=none|info|debug` controls logging. Checks print file and finding
counts on stderr by default. When findings exist, the summary also counts them per
source file and per rule/diagnostic code, sorted by count. `--summary=false` disables the summary.
`--quiet` suppresses logs and summaries while preserving requested results
and errors. In JSON modes, stderr contains newline-delimited records for logs,
summaries and errors. Check failures use the same result contract, with
`completed: false`, `error`, and `exit_code`. Metadata commands keep their own
error records. Log
records have `log`, and the summary has `summary.files` and `summary.findings`.
Nonempty summaries also include `summary.by_file` (`path`, `count`) and
`summary.by_diagnostic` (`rule`, optional `code`, `count`).
Check the exit status before interpreting empty stdout as success.

| Exit status | Meaning                                                                                                   |
| ----------- | --------------------------------------------------------------------------------------------------------- |
| `0`         | Completed without findings that affect the exit status, or completed an information/configuration command |
| `1`         | Completed with findings that affect the exit status                                                       |
| `2`         | Invalid CLI arguments or incompatible options                                                             |
| `3`         | Could not initialize or complete, including invalid config, regex or template                             |

`--help --json` describes commands, flags, choices, defaults and exit codes.
`version --json` exposes build metadata.

Use `--verbose` or `--debug` to include version and build information in the log.

When stdout is a terminal and `jq` is installed, JSON metadata and JSON/SARIF
diagnostics are indented and colored according to the color controls. Use
`--json-pretty=false` to disable this. Pipes and files require an explicit
pretty flag. Missing or failing `jq` falls back to the
original JSON. It is never installed automatically.

Explicit `--json-pretty` also selects JSON when no other output format is set.
For example, `actionlint rules --json-pretty` prints the rules as JSON. The default
value and `--json-pretty=false` do not change the selected output format.
Explicit `--json-pretty` also indents JSON written to pipes and files, using
built-in formatting without color codes or a jq dependency.

CLI defaults can also come from [environment variables](env.md), including
configuration selection, output, logging and external linters. Explicit flags
take precedence, including empty strings and `false`.

Actionlint allows network requests by default. `--offline` blocks them for every
command, including plugin downloads, registry lookups and remote configuration.
Use `--online` to select the default explicitly. These flags cannot be combined.

Before disconnecting, download the dependencies for the files you will check:

```sh
actionlint cache warm .github/workflows
actionlint check --offline .github/workflows
```

`cache warm` reads the selected configuration and its `extends`, downloads its
plugins, and fetches metadata for remote actions and reusable workflows.
It prints the cache directory to save in CI. It fails if selected action metadata
cannot be cached. Lint findings do not affect this command's exit status.

Offline checks use cached configuration, verified plugin artifacts and remote
metadata. Tags and branches use their last cached resolution. Run online again
to refresh them. Missing dependencies produce warnings. If a required config or
plugin is missing, actionlint warns that the check could not complete and returns
zero. Missing action metadata appears in JSON coverage, and checks that have the
data they need still run. Invalid configuration and damaged plugin artifacts
remain errors.

Installed native linters, including ShellCheck and Ruff, still run offline.
Actionlint blocks its downloads and does not start remote resolver processes.
It does not sandbox network access inside native tools. Cached WebAssembly
plugins can still run.

Online metadata lookups read the cache, then
`github.com` and `raw.githubusercontent.com`. If the raw host fails, the mirror
settings determine whether actionlint tries `cdn.jsdelivr.net` before GitHub REST.
By default, only repositories known to be public can use that mirror.
See [remote action metadata](remote-actions.md) for the supported
references and resolver configuration.

### Network accounting

Use `--network-stats` to write grouped network statistics as a final JSON record
on stderr. Diagnostic JSON on stdout is unchanged:

```sh
actionlint check --network-stats --output=json .github
```

Each group identifies the host, request kind, purpose and priority. The `credentials`
field is `provided` when credentials were supplied, `absent` when none were supplied,
or `unknown` when an external process did not report them. It does not prove that
the credentials were accepted. Reports exclude credential values, request paths and
query strings.

`attempts` counts HTTP transport attempts or provider-reported Git operations.
Retries and redirects are separate attempts at the instrumented transport.
`failures` counts transport errors and HTTP error responses. `quota_rejections` counts
rejected requests or batch admission checks, before transport, because of observed
limits or reservations. A rejected batch counts once, regardless of its size.
`cache_hits` records cached or coalesced external resolver calls.
`resolver_calls` counts resolver calls independently of their network attempts.
`unreported_resolver_calls` counts calls whose internal network activity is not
fully reported; these are not presented as HTTP request counts.
`resolver_reported_attempts` identifies attempts reported by an external resolver.

GitHub metadata acquisition reads Git reference lists from `github.com` (request
kind `git`) and manifests from `raw.githubusercontent.com` (request kind `http`).
When `raw.githubusercontent.com` fails, it reads manifests from
`cdn.jsdelivr.net` (request kind `http`, credentials `absent`). These requests
do not use the REST API quota. When metadata acquisition falls
back to the REST API, it checks `/rate_limit` once per credential scope before
the first REST request and updates estimates from subsequent response headers.
REST and GraphQL quotas are separate. Primary exhaustion and `Retry-After` stop
further applicable requests without sleeping through the reset window. A plain
permission-denied response does not imply rate-limit exhaustion. These controls
follow [GitHub's rate-limit guidance](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api).

An HTTP caller can reserve quota units for later required requests. The tracker
then blocks lower-priority requests from spending those units. A quota check is
not a guarantee: another process can use the same server quota after actionlint
observes it. Unknown quota information does not mean the quota is zero. These
statistics belong to one invocation and are not lint configuration settings.

External resolvers can add a `network` request report to their response:

```json
{
  "network": {
    "all_requests_reported": true,
    "requests": [
      {
        "host": "github.com",
        "request_kind": "git",
        "credentials": "unknown",
        "attempts": 1,
        "failures": 0
      },
      {
        "host": "api.github.com",
        "request_kind": "rest",
        "credentials": "provided",
        "attempts": 2,
        "failures": 0
      }
    ]
  }
}
```

Add this field to the resolver's response alongside its resolution results.
Set `all_requests_reported` only when every remote attempt is included,
including failures. Supported kinds are `http`, `rest`, `graphql`, `git` and `oci`.
The host is an authority without userinfo, a path or a query. Old resolvers work
without this field, but their calls are counted under `unreported_resolver_calls`. In-process providers can
use the shared HTTP transport. Actionlint cannot observe network requests made
directly by subprocesses or WebAssembly guests.

### Configuration inspection

```sh
actionlint check --config=other.yml workflow.yml
actionlint check --no-config workflow.yml
actionlint config init
actionlint config init --toml
actionlint config init --jsonc
actionlint config path
actionlint config show --origin
actionlint config validate
actionlint config schema
```

Select an explicit configuration file or use repository discovery in this order:
`.github/actionlint.yaml`, `.github/actionlint.yml`, `.github/actionlint.json`, `.github/actionlint.jsonc`, then
`.github/actionlint.toml`. `ACTIONLINT_CONFIG` and
`ACTIONLINT_NO_CONFIG` provide selection defaults; explicit config flags override
both. `--no-config`, `--no-rc` and `--norc` skip project configuration loading.
They still load [shared network policies](config.md#shared-defaults-and-restrictions).
`ACTIONLINT_NETWORK_POLICY` selects an additional policy file or HTTPS URL.
Machine and user policy files apply even when that variable selects another file.
An explicit `check --config` also avoids parsing an
unselected repository config.

`config path` prints the selected path, or nothing when none is selected. It can
identify an invalid file without parsing it. `config show` prints effective YAML;
`--json` returns a document with `path` and `config`. `--origin` adds an `origins`
map keyed by JSON Pointer. Sources include `default`, `config` and `policy`, and
states distinguish missing keys, explicit `null` and explicit values such as
`false` and `[]`. Config sources include line and column positions. A default
value does not imply that the user explicitly set it.

`config validate` uses the same parser and validation as analysis, including its
current handling of unknown keys described in [Configuration](config.md). It does
not silently introduce the stricter JSON Schema validation policy.
`config schema` prints the configuration JSON schema with an entry under `tools`
for each tool provider that `--plugin` or the configuration installs; see
[Configuration](config.md).
`config init` creates `.github/actionlint.yaml`. Use `--json`, `--jsonc`, `--toml`,
`--yaml` or `--yml` to choose the format and extension. The flags cannot be combined.
The generated file includes its editor schema URL. YAML, JSONC and TOML also
include commented rule examples generated from the rule declarations.
The command refuses to create another config when any supported config file exists.
`init`, `init-config`, `--init` and `--init-config` are compatibility aliases;
`config --init` and `config --init-config` also work. The success message displays
a path relative to the working directory, linked to the file when terminal
hyperlinks are enabled. Put `--json` before `config init` to receive the absolute
path as JSON, for example `actionlint --json config init --toml`.

`rules` lists checks by name, description and category; `rules NAME` explains one
check. `doctor` reports the selected config and resolves configured external-tool
commands without executing them. Its paths become clickable `file://` links when
hyperlinks are enabled, with spaces and non-ASCII characters encoded in the target.
Windows drive and UNC paths are supported. Missing optional tools are reported as unavailable;
a malformed selected configuration returns exit status 3. Help, version and
completion do not load configuration or create a linter. Rules load selected plugin
declarations without running analysis.

### Shell completion

`completion <shell>` prints a completion script for the given shell to stdout.
`completions` command is an alias; either command without a shell shows help.
Help examples follow the same shell detection as `completion auto`: a supported
`SHELL` wins, then the presence of `PSModulePath` selects PowerShell. When neither
signal resolves, help lists explicit shell choices without shell-specific loading syntax.
Executable suffixes follow `PATHEXT`, matched case-insensitively. When it is unset
or empty, the Windows fallback is `.COM;.EXE;.BAT;.CMD`.
Supported shells are `bash`, `fish`, `powershell` and `zsh`. The value also
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

Every registered diagnostic rule supports [inline exceptions](config.md#inline-diagnostic-suppressions)
with a rule name and an optional reason. Place the comment on the reported line, or use
`actionlint:ignore-next-line` immediately before it. A directive on a YAML block scalar header covers its body.
Trailing comments also accept `# actionlint-ignore RULE`, `# ignore RULE` and `# ignore[RULE]`.
The bare `# ignore RULE` form requires a known first rule name. Comments such as
`# ignore docs as they are built elsewhere` are ordinary comments.
Separate multiple rule IDs with commas, spaces, or both. The preceding-line form
also accepts `# actionlint-ignore-next-line RULE` and `# ignore-next-line RULE`.

`unused-suppression` reports each inline selector that matches no enabled findings.
It is informational by default and an error with `--strict`; explicit rule settings
still take precedence. Script-internal directives such as `# shellcheck disable=...`
belong to their tool and are not audited by this rule.
Use `actionlint fix --apply` or the editor quick fix to remove unused selectors.
Selectors that suppress findings are preserved, along with the directive spelling,
reason and line endings. A suppression comment is removed when all its selectors
are unused.

```yaml
cache-mode: write # actionlint:ignore cache-write-untrusted -- this job runs reviewed default-branch code only
```

Local actions must have an `action.yml` or `action.yaml` available when linting.
For generated actions, [map the runtime path to its source](config.md#local-action-source-mappings)
to validate the checked-in manifest. For intentional exceptions, use a short comment:

```yaml
- uses: ./generated-action # actionlint:ignore missing-local-action
```

[Reusable ignores](config.md#reusable-ignores) let you declare an exception once
for matching files, jobs, steps or action references.

Use `--ignore` to filter findings by exact rule name or diagnostic code. Repeat
the flag to ignore several rules or codes. A bare code matches any rule that
emits it. Use `rule/code` to limit the match to one rule. Rule aliases also work.
Ignored findings do not affect the exit status or summary and their fixes are
excluded from `actionlint fix`.

```sh
actionlint check --ignore missing-permissions --ignore SC2086 workflow.yml
actionlint check --ignore shellcheck/SC2086 workflow.yml
```

`--ignore` and `-ignore` previously accepted message regexes. Use
`--ignore-regex` for those patterns.

`--ignore-regex` filters findings by regular expression. It
matches the message and, when a diagnostic has a code, `CODE: message`. Repeat
the option to add patterns. The syntax is the same as [RE2][re2].

```sh
actionlint --ignore-regex 'SC2086:' --ignore-regex 'label ".+" is unknown'
```

External tools are selected by id. The bundled tools are `shellcheck` and `ruff`;
plugins may add more. `--tool ID` runs a tool with its own command on `PATH` and
fails when that command cannot run. `--tool ID=COMMAND` accepts a command name, path,
or command line with arguments. `--no-tool ID`, `--tool ID=false` and
`--tool ID=` disable the tool, including plugins that provide it.

```sh
actionlint --no-tool shellcheck
actionlint --tool shellcheck='shellcheck -e SC2086'
```

ShellCheck and Ruff use their registry Wasm plugins by default. The first online
check downloads them into the user cache. Later checks reuse that selection and
the verified artifacts, including with `--offline`. Run `actionlint cache warm`
to refresh and cache the current registry releases before an offline check.
Use `--tool shellcheck` or `--tool ruff` to select an installed native executable.
See [Ruff configuration](config.md#ruff) for Python checks.
The last `--tool` or `--no-tool` for an id wins and
replaces the tool's environment settings. Legacy single-dash spellings follow the
same rules at the root and after subcommands. A plugin's ids are its lock entry's
name and alias and the engine and rule names in its metadata. An id that names no
bundled tool and no plugin is rejected with the list of valid ids.

The actionlint v1 option `-shellcheck COMMAND`, which the bundled ShellCheck tool
declares, remains as a hidden, deprecated alias of `--tool shellcheck=COMMAND`; an
empty command disables ShellCheck. Each use prints a deprecation warning.

Repository configuration can select native executables through
`tools.<name>.backend.process`. Authorize these selections with
`--allow-config-processes` only when you trust the complete configuration,
including inherited files and overrides. Explicit CLI and environment commands
already authorize their own executable.

The deprecated actionlint v1 option `-pyflakes COMMAND`, which the bundled Ruff tool declares,
selects Ruff's Pyflakes (`F`) rules when nothing else selects Ruff. The command
value is never executed. An empty value or `false` disables Ruff.

To configure executables, arguments and child environments separately, use
`ACTIONLINT_TOOL_<ID>_BIN`, `ACTIONLINT_TOOL_<ID>_FLAGS`, and
`ACTIONLINT_TOOL_<ID>_ENV`, where `<ID>` is the tool id upper-cased with dashes
replaced by underscores, such as `ACTIONLINT_TOOL_SHELLCHECK_BIN`.
`--tool` and `--no-tool` override all three environment settings for that tool.
See [External linter environment settings](env.md#external-linters) for quoting,
Windows paths and examples.

ShellCheck runs for recognized shell interpreters or scripts selected by a leading
native `shell` directive. Configured dialects and command flags alone do not select
unknown or non-shell scripts.

ShellCheck commands, including wrappers, run in the step's resolved working
directory so sourced files resolve as they do on the runner. Arguments are passed
literally; use absolute paths for wrapper-owned files. For ShellCheck rc files,
use [`tools.shellcheck.config`](config.md#shellcheck), which resolves paths
relative to the actionlint configuration file.

Your arguments are prepended to actionlint's integration arguments. actionlint
owns the JSON1 output format and stdin input, so do not pass `-f`/`--format` or
file arguments. It supplies its default exclusions, rc-file selection and source
following settings. An inferred `--shell <dialect>` is added only when no explicit
ShellCheck flag or leading `shell` directive selects the dialect. ShellCheck
arguments take precedence over `SHELLCHECK_OPTS`, followed by a leading `shell`
directive, actionlint's configured dialect and the workflow-derived dialect. See
[script selection and directives](config.md#script-selection-and-directives).

By default, `--norc` disables rc discovery. Set
[`tools.shellcheck.config`](config.md#shellcheck) to an inline mapping or an rc
file/directory; an rc path replaces `--norc` with `--rcfile`. Following sourced
files (`-x`) can be disabled in
configuration and is disabled when the step's working directory cannot be resolved
locally. Additional options can use `--tool shellcheck='<command line>'` or the
[`SHELLCHECK_OPTS` environment variable](checks.md#check-shellcheck-integ).

<a id="format"></a>

### Format error messages

Use `-format` to format error messages with [Go template syntax][go-template].

The following examples show how to format diagnostics.

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
{{range $err := .}}::error file={{escapeProperty $err.Filepath}},line={{$err.Line}},col={{$err.Column}}::{{escapeData $err.Message}}%0A```%0A{{escapeData $err.Snippet}}%0A```
{{end}}
' --ignore SC2016
````

Output:

For a reference to `missing.value` on line 6 of `.github/workflows/ci.yml`, the
template emits this annotation command:

````text
::error file=.github/workflows/ci.yml,line=6,col=24::undefined variable "missing". available variables are "env", "github", "inputs", "job", "matrix", "needs", "runner", "secrets", "steps", "strategy", "vars"%0A```%0A      - run: echo '${{ missing.value }}'%0A                       ^~~~~~~%0A```
````

`escapeData` encodes `%`, carriage returns and newlines in the annotation body.
`escapeProperty` also encodes `:` and `,` in property values such as the file path.
Both follow [`@actions/core` workflow-command escaping](https://github.com/actions/toolkit/blob/main/packages/core/src/command.ts).
The example suppresses ShellCheck's `SC2016` diagnostic about the template argument.

Use `actionlint --output github` for standard annotations. Templates let you
customize the annotation body.

#### Example: [SARIF format][sarif]

[The Static Analysis Results Interchange Format (SARIF)][sarif] is a standardized format for the results of static analysis tools.

Use the built-in format directly:

```sh
actionlint --output sarif > actionlint.sarif
```

Native SARIF output is a typed JSON document.

See the [SARIF output example](../internal/development/tests/integration/output/testdata/format/test.sarif).

#### Formatting syntax

In [Go template syntax][go-template], `.` within `{{ }}` means the target object. Here, the target object is a sequence of error
objects.

Use the `range` action to traverse the sequence, as you would use `for ... = range ... {}` in Go.

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
| `{{$err.Code}}`      | Native diagnostic identifier, when supplied           | `SC2086`                                                            |
| `{{$err.Severity}}`  | Diagnostic level; empty means error                   | `info`                                                              |
| `{{$err.Filepath}}`  | Canonical relative file path of the error position    | `.github/workflows/ci.yml`                                          |
| `{{$err.Line}}`      | Line number of the error position (1-based)           | `9`                                                                 |
| `{{$err.Column}}`    | Column number of the error's start position (1-based) | `11`                                                                |
| `{{$err.EndColumn}}` | Column number of the error's end position (1-based)   | `23`                                                                |

<!-- dprint-ignore-end -->

Functions called inside `{{ }}` are template actions. In addition to the actions
defined by the Go standard library, actionlint provides the following custom
actions. The JSON example above uses `json`.

| Action             | Description                                                                      | Example usage                             |
| ------------------ | -------------------------------------------------------------------------------- | ----------------------------------------- |
| `json x`           | Serialize `x` as JSON string followed by newline character                       | `{{json $err}}`                           |
| `replace x y z`    | Replace string `y` with `z` in `x`                                               | `{{replace $err.Filepath "\\" "/"}}`      |
| `escapeData x`     | Escape string `x` for a GitHub Actions command body (`%`, CR, LF)                | `{{$err.Snippet \| escapeData}}`          |
| `escapeProperty x` | Escape string `x` for a GitHub Actions command property (also `:` and `,`)       | `{{$err.Filepath \| escapeProperty}}`     |
| `toPascalCase x`   | Convert `x` into PascalCase (e.g. 'foo-bar' to 'FooBar')                         | `{{toPascalCase $err.Kind}}`              |
| `allKinds`         | Return an array of kind objects. The kind object is explained in the below table | `{{range $ = allKinds}}{{$.Name}}{{end}}` |
| `getVersion`       | Return the version of actionlint as string                                       | `{{getVersion}}`                          |

The `allKinds` action returns kind objects with the following fields.

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

Inside `{{ }}`, you can pipe a value to an action to transform it. In the
example above, `|` pipes the message to `printf "%q"`.

Backslash escapes such as `\n` in the format string are automatically unescaped.

### Exit status

The `actionlint` command exits with one of the following statuses.

| Status | Description                                                                |
| ------ | -------------------------------------------------------------------------- |
| `0`    | The check completed without findings that affect its exit status           |
| `1`    | The check completed with at least one finding that affects its exit status |
| `2`    | The command failed due to invalid command line option                      |
| `3`    | The command failed due to some fatal error                                 |

Advice from downloaded actions remains visible but does not affect the exit
status by default. Known execution errors in those actions still count.
Use [`lint.dependencies`](config.md#findings-in-dependencies) to include other
dependency findings. Findings in your workflows and local actions count as usual.

<a id="on-github-actions"></a>

## Use actionlint on GitHub Actions

Start with the [Action guide](action.md): quick start, configuration, saved reports,
optional PR reviews, tools and the complete input/output reference.

### Manual installation

Use the download script for direct access to the executable. It sets the absolute path of the downloaded
executable to the `executable` output for following steps.

This workflow runs actionlint on GitHub Actions. Set `shell: bash` because the
default shell on Windows runners is `pwsh`.

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
        run: bash <(curl -fsSL https://raw.githubusercontent.com/kjanat/actionlint/662318dd6bbd9c0c120e35b03168bc1be69bf428/scripts/download-actionlint.bash) 2.0.0
        shell: bash
      - name: Check workflow files
        env: { actionlint: "${{ steps.get_actionlint.outputs.executable }}" }
        run: '"${actionlint}" --output github'
        shell: bash
```

Or simply download the executable and run it in one step:

```yaml
- name: Check workflow files
  run: |
    bash <(curl -fsSL https://raw.githubusercontent.com/kjanat/actionlint/662318dd6bbd9c0c120e35b03168bc1be69bf428/scripts/download-actionlint.bash) 2.0.0
    ./actionlint --output github
  shell: bash
```

The download script accepts an actionlint version and a download directory.
Pass `--help` to the script for more details.

If you want to enable
[shellcheck integration](checks.md#check-shellcheck-integ), install `shellcheck`
command. Note that shellcheck is
[pre-installed on Ubuntu worker][preinstall-ubuntu].

`--output github` emits [GitHub annotations](#github-annotations) directly.

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
          args: --output github
```

## Online playground

The actionlint playground runs in your browser using WebAssembly. It does not
send data outside your browser.

<https://kjanat.github.io/actionlint>

Paste your workflow into the editor on the left. Results appear on the right and
update as you edit. Click an error in the results table to move the cursor to its
location in the editor.

<a id="docker"></a>

## [Docker][docker] image

[![Docker Image Version][docker-badge]][dockerhub]

[Docker image][docker-image] is available. Images built from this revision contain `actionlint`,
ShellCheck, and the native Ruff executable. No Python interpreter is required.
Ruff's official release archive is pinned by version and SHA-256 for both Linux amd64 and arm64.
Previously published images are unchanged; Ruff will be included in the next release image.

Available tags are:

- `ghcr.io/kjanat/actionlint:latest`:\
  Moving alias for the latest stable version of actionlint. This image is recommended.
- `ghcr.io/kjanat/actionlint:{version}`:\
  Release-specific actionlint image rather than a moving alias.\
  (e.g. `ghcr.io/kjanat/actionlint:2.0.0`)

The CLI image is also published to Docker Hub as `kjanat/actionlint:latest` and
`kjanat/actionlint:{version}`. Both registries carry the same manifest, so pick
whichever your setup pulls from more easily. Older action refs use `action-*`
images; new releases use the JavaScript action.
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

To check a file in a Docker container, pass its contents through stdin and use `-`:

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

The `action-*` images are the exception. Their `shellcheck` input is a boolean that switches the integration on or off, so
`SHELLCHECK_OPTS` in the step's `env:` is the only way to configure shellcheck
there.

## Using actionlint from Go program

Use the [integration interfaces](api.md) to connect other programs or write plugins.

<a id="tools-integ"></a>

## Tools integration

### reviewdog

[reviewdog][reviewdog] posts inline review comments and filters findings to changed
lines, keeping feedback focused on the pull request.
The [`reviewdog/action-actionlint` action][reviewdog-actionlint] runs this fork
with ShellCheck enabled.
We recommend v1.76.2 or newer.

Add the following workflow to `.github/workflows/reviewdog-actionlint.yaml`:

```yaml
name: reviewdog-actionlint
on: [pull_request]
jobs:
  actionlint:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: write
    steps:
      - uses: actions/checkout@v7
        with: { persist-credentials: false }
      - uses: reviewdog/action-actionlint@v1
        with:
          reporter: github-pr-review
          filter_mode: added
          fail_level: any
```

This posts review comments for findings on added or modified lines and fails the
step when those lines have problems. Set `filter_mode: diff_context` to include
the surrounding diff context, or `fail_level: none` to leave comments without
failing the step. Invalid options and fatal errors always fail the step.

For pull requests from forks, reviewdog falls back to GitHub annotations when the
workflow token cannot post review comments.

Pass additional actionlint options through `actionlint_flags`. See the
[action documentation][reviewdog-actionlint] for all inputs and available reporters.

<a id="problem-matchers"></a>

### GitHub annotations

The [GitHub Action](action.md) emits annotations by default. With an installed
CLI, select GitHub output:

```yaml
- name: Check workflow files
  run: actionlint check --output github
```

This writes annotation commands directly to the runner, including findings from plugins.

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

### Native Git hooks

[Git 2.54 and newer](https://git-scm.com/docs/git-hook/2.54.0) let you register named hooks directly in Git
configuration. To run actionlint before each commit, [install actionlint](install.md) on your `PATH`, then run
these commands in your repository:

```sh
git config set --local hook.actionlint.command 'actionlint pre-commit'
git config set --local hook.actionlint.event pre-commit
```

`actionlint pre-commit` ignores the arguments Git passes to a hook, so the same command also works for events
such as `pre-push`.

Git runs actionlint when you commit. A lint failure blocks the commit. This checks discovered workflows and actions in the
working tree, including unstaged edits; it does not check an isolated copy of the staged files.

List the registered hooks and try them before committing:

```sh
git hook list --show-scope pre-commit
git hook run pre-commit
```

The settings are local to this checkout and are not copied when someone clones the repository. Git runs the
installed binary; you manage its installation and updates yourself. An existing `.git/hooks/pre-commit`
script also runs, after the configured hooks.

To disable only the actionlint hook, keeping its configuration:

```sh
git config set --local hook.actionlint.enabled false
```

Set the same key to `true` to enable it again.

[Git 2.55](https://git-scm.com/docs/git-hook/2.55.0) adds parallel execution for eligible hook events and
event-wide controls. The `pre-commit` event always runs its hooks sequentially.

For a discussion of native hooks and the pre-commit framework, see
[does git 2.54 make pre-commit obsolete?](https://www.youtube.com/watch?v=8Jwg_mM9WYc) by anthonywritescode.

### pre-commit

For v2, [pre-commit][pre-commit] hooks are published separately in
[kjanat/actionlint-pre-commit](https://github.com/kjanat/actionlint-pre-commit).
The first v2 hook release is pending; replace the placeholder below with a published
tag from that repository before installing:

```yaml
repos:
  - repo: https://github.com/kjanat/actionlint-pre-commit
    rev: <published-v2-tag>
    hooks:
      - id: actionlint
```

The default `actionlint` hook installs an exact `actionlint-kjanat` wheel dependency
in pre-commit's Python environment. The wheel contains the native host binary; no Go
compiler or separate native ShellCheck/Ruff installer is needed. The project's
[plugin declarations](plugins.md) select exact registry releases or digest-pinned Wasm artifacts.
Initialize or migrate those files with actionlint before committing them.

The distribution also offers `actionlint-system` for an existing installation and
`actionlint-docker` for its pinned published container. See the hook repository for
platform support and release prerequisites.

All hooks run `actionlint pre-commit` on every commit and check **all repository
workflows**, including when only a local action, reusable workflow, or configuration
is changed or deleted. They use `always_run: true` and `pass_filenames: false`.
The native command succeeds when the repository has no workflows and otherwise
performs ordinary repository analysis. Only referenced local actions receive
metadata validation; missing actions report `missing-local-action`.

#### Updating pre-commit hooks

Change the `repo:` URL explicitly when migrating from v1: `pre-commit autoupdate`
does not move a configuration to another repository. For v1 hooks, use the old
repository URL. See [v2 migration](migration-v2.md).

After the first v2 hook release, update within the dedicated repository:

```sh
pre-commit autoupdate --freeze --repo https://github.com/kjanat/actionlint-pre-commit
```

Only completed stable hook releases receive tags in that repository; unrelated
source, Action-channel, and development tags belong to the host repository.
Review the selected release and commit the updated revision. A frozen revision pins
the hook installation; exact declarations in actionlint configuration select the project's plugins.

### VS Code

> [!NOTE]
> The extension runs the command configured in `linter-actionlint.config`,
> which defaults to `actionlint` on `PATH`. It uses this fork when that command
> resolves to a fork build; it does not download actionlint itself.

The [Linter extension][vsc-extension] for [VS Code][vscode] detects the
`.github/workflows` directory, runs `actionlint`, and reports errors in the
editor as you edit workflow files.

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

[nvim-lint][nvim-lint] runs actionlint asynchronously and reports errors as you
edit GitHub Actions workflows in Neovim. See the plugin documentation for details.

[ALE][vim-ale] runs actionlint asynchronously and reports errors as you edit
GitHub Actions workflows in Vim or Neovim. See the plugin documentation for details.

### Pulsar Edit

> [!NOTE]
> The package runs the local executable configured by `actionsExecutablePath`,
> which defaults to `actionlint` on `PATH`. Point that setting at this fork's
> binary to use fork-specific checks.

The [Linter package][pulsar-linter] for [Pulsar Edit][pulsar] detects a
`workflows` directory, runs `actionlint` on GitHub Actions files in that directory,
and shows its findings in the editor as you work.

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
trunk check enable actionlint@2.0.0
```

or modify `.trunk/trunk.yaml` in your repository to contain:

```yaml
lint:
  enabled:
    - actionlint@2.0.0
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

[Checks](checks.md) | [Installation](install.md) | [Configuration](config.md) | [Integration interfaces](api.md) | [References](reference.md)

[cmd-manual]: https://kjanat.github.io/actionlint/usage.html
[docker-badge]: https://img.shields.io/docker/v/kjanat/actionlint?logo=docker&logoColor=white&label=Docker&labelColor=2496ED&color=black
[dockerhub]: https://hub.docker.com/r/kjanat/actionlint
[docker-image]: https://github.com/kjanat/actionlint/pkgs/container/actionlint
[docker]: https://www.docker.com/
[emacs-flycheck-extension]: https://github.com/tirimia/flycheck-actionlint
[emacs-flycheck]: https://www.flycheck.org/
[emacs-flymake-extension]: https://github.com/ROCKTAKEY/flymake-actionlint
[emacs-flymake]: https://www.gnu.org/software/emacs/manual/html_node/flymake/
[emacs-melpa]: https://melpa.org/
[ga-annotate-error]: https://docs.github.com/en/actions/learn-github-actions/workflow-commands-for-github-actions#setting-an-error-message
[go-template]: https://pkg.go.dev/text/template
[jsonl]: https://jsonlines.org/
[nova-extension]: https://extensions.panic.com/extensions/org.netwrk/org.netwrk.actionlint/
[nova]: https://nova.app
[nvim-lint]: https://github.com/mfussenegger/nvim-lint
[pre-commit]: https://pre-commit.com
[preinstall-ubuntu]: https://github.com/actions/runner-images/blob/main/images/ubuntu/Ubuntu2404-Readme.md
[pulsar-linter]: https://web.pulsar-edit.dev/packages/linter-github-actions
[pulsar]: https://pulsar-edit.dev/
[re2]: https://golang.org/s/re2syntax
[release-badge]: https://img.shields.io/github/v/release/kjanat/actionlint?logo=github&logoColor=white&label=release&labelColor=181717&color=black
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

## Editor, fixes and remote resolution

`actionlint lsp` serves LSP over stdin/stdout. It supports UTF-16 positions,
versioned open buffers, ranged changes, diagnostic publication, and versioned
quick-fix edits. Open document changes invalidate analysis of open dependants.
The client applies edits; the server never writes editor buffers to disk.

For an allowed diagnostic, choose **Suppress RULE for this declaration** from the
editor's quick-fix menu. The action adds a suppression comment or extends an
existing one while retaining its reason. Multiline scripts are suppressed at the
YAML declaration. The target buffer must be open; edits carry its current version.
Rules prohibited by `forbidden-suppression` do not offer this action.

`actionlint fix [files...]` previews source edits as JSON. `--apply` writes the
validated edits and checks the files again. Only fixes marked safe are selected
by default; `--include-suggested` also includes edits requiring review. Conflicting
edits or stale source revisions abort preparation. Writes are atomic per file;
an I/O failure during a multi-file application reports how many files were written.

`actionlint plugin search` discovers published plugins. `plugin install owner/repo@TAG`
adds the canonical package and exact version to the config's root `plugins` list;
commit that file. Registry evidence and verified resolutions stay in the user cache.
Check, fix, rules and LSP load declared plugins using their pins. `plugin install`
without a name resolves manually edited declarations or migrates older lock-only projects.
`plugin sync` restores exact pins; `plugin update` explicitly refreshes declared versions.
See [plugin installation](plugins.md#install-from-the-registry)
for version selection, overrides and offline behavior.

`--plugin PATH` loads a WebAssembly plugin. A `.json` path loads a
[local tool provider manifest](plugins.md#local-tool-provider-manifests) that runs a native command.

Online metadata lookup uses the bundled Commitish WebAssembly plugin to resolve
branches and tags from each repository's Git reference list. Actionlint reads
cached manifests first. If the raw file host fails, actionlint can request the
same file from jsDelivr when the mirror settings allow it. By default, the
repository must be known to be public. Any raw-host `404` prevents a jsDelivr
request. GitHub REST handles other failed lookups and verifies object types
that the Git reference listing cannot establish. Actionlint includes every request in
`--network-stats`. Use
`--offline` for cached metadata or `--online` for the default behavior.

Load a replacement resolver component with `--plugin`. To use a native resolver,
select it with `--resolve-with EXECUTABLE`; repeat `--resolver-arg VALUE` for
literal arguments. This option also enables commit-pin suggestions. See
[remote metadata and reference resolution](remote-actions.md).

The language server provides expression property completions, including
`github.event` payload fields selected by the workflow's triggers and activity
filters. Dot access, quoted bracket access, array indices and wildcard access
use the same types as expression diagnostics. Completions use the current editor
buffer, including unsaved changes. Unknown events and caller-provided payloads
allow arbitrary properties; conditions do not yet narrow the event type within a branch.

Expression findings have independent rule settings. For example, allowing unknown
payload properties does not disable script-injection checks:

```yaml
lint:
  rules:
    correctness:
      event-payload: off
      expression-reference: error
    security:
      script-injection: error
```

The former `expression` catch-all has been split into `expression-syntax`,
`expression-reference`, `context-availability`, `expression-type`,
`expression-call`, `event-payload` and `script-injection`. Update existing
suppressions and severity settings to the specific check. The `expression` rule
now covers expression values and interpolation forms required by workflow fields.
See the [rule reference](rules.md) for each rule's configuration group and help.
