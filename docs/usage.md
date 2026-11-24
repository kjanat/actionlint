# Usage

## `actionlint` command

With no arguments inside a Git repository, `actionlint` checks the repository's workflows and
`action.yml`/`action.yaml` manifests. Outside a repository, it prints help and exits 0.
`actionlint check` reports an error outside a repository. If discovery finds no inputs, the check exits 3.

```sh
actionlint check
```

Pass workflow paths to check only those files.

```sh
actionlint path/to/workflow1.yaml path/to/workflow2.yaml
```

A directory argument is a search root. actionlint finds action manifests and YAML files below
`.github/workflows`, including in nested repositories. Each repository uses its own Git ignore rules,
configuration and plugins.

Directories and globs respect configured file filters and Git ignore rules. A file named literally
overrides both. Use `--no-ignore` to scan ignored files. Quote globs so actionlint expands them the
same way in every shell.

```sh
actionlint .
actionlint .github/
actionlint check .github/workflows/ .github/actions/
actionlint '~/projects/*'
actionlint ~/projects --no-ignore
```

Use `-C DIR` to run from another directory. Repeated relative `-C` values build on the previous one:

```sh
actionlint -C path/to/repository .github/
actionlint -C path/to/repository -C testdata .
```

You can mix workflows and actions in one invocation:

```sh
actionlint .github/workflows/ci.yml .github/actions/build/action.yml
```

Use [file exclusions](config.md) to omit fixtures or generated actions.

Pass `-` to read workflow source from stdin.

```sh
cat path/to/workflow.yaml | actionlint -
```

`actionlint --help` lists common options. `actionlint check --help` lists all checking options. See
[the online command manual][cmd-manual] for the full reference.

### Commands and compatibility

Options may come before or after filenames. Legacy single-dash names such as `-format` still work.
Use `--` to treat every remaining argument as a filename.

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

Use `--stdin-filename=action.yml` to check action metadata from stdin.

```sh
actionlint -- --workflow.yml
actionlint ./version
actionlint check version --json
```

`--help-legacy` lists the old option names.

`check --color=auto|always|never` controls color. Bare `--color` means `always`.

Pretty diagnostics make URLs clickable through OSC 8 terminal hyperlinks.
`--hyperlinks=auto|always|never` controls them.

```sh
actionlint --hyperlinks=always --help
actionlint check --hyperlinks=never --help
actionlint doctor --hyperlinks=always
```

Put root color and hyperlink flags before `--help`.

Through npm, these forms pass the help flag to actionlint:

```sh
npx @kjanat/actionlint --help
npm exec --package=@kjanat/actionlint -- actionlint --help
```

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
`pretty` is the default. Repeat `--output-format` to render several formats in order.

Set `output.formats`, `output.log-level`, `output.summary`, `output.network-stats` and `output.quiet`
in the configuration file to change the defaults. CLI flags override environment settings, which
override configured defaults.

`gcc` emits one `file:line:column: severity: message [code]` line per finding. `--template`, its
alias `--format`, and `--template-file` render [Go templates](#format-error-messages). A template
cannot be combined with a built-in output format.

```sh
actionlint check --output-format=pretty --diagnostic-width=80 --color=auto
```

`--diagnostic-width` accepts 40 to 400 columns and requires `pretty`. The default is 100. Each
`pretty` diagnostic shows its rule's configuration path. `actionlint rules RULE` explains the rule.

JSON returns a versioned document. Positions use one-based Unicode character columns, and the end
position is exclusive.

The CLI and the Action share [one result contract](results.md). JSON Lines emits one diagnostic per
line and nothing for a clean result.

Check results go to stdout or to `--output-file PATH`. actionlint replaces a report only after analysis completes. Logs and errors go to
stderr.

```sh
actionlint --strict
actionlint --experimental
actionlint check --strict --experimental
```

`--strict` enables stable `correctness`, `security` and `suspicious` rules. `--experimental`
enables nursery rules. Explicit rule exceptions in configuration still apply.
`lint.experimental: true` enables nursery rules in configuration.

`--log-level=none|info|debug` controls logging. Checks print a file and finding summary on stderr.
`--summary=false` disables the summary, and `--quiet` suppresses logs and summaries. In JSON modes,
stderr carries newline-delimited JSON records. Check the exit status before you treat empty stdout
as success.

Findings from remote actions carry `origin: "dependency"` in JSON and a `dependency:` prefix in
pretty output, such as `info[dependency:shellcheck] SC2086`. `--dependency-findings=full` shows
each one in full.

`--help --json` describes commands, flags and exit codes. `version --json` prints build metadata.

`--verbose` and `--debug` log the inputs and settings behind the findings. Compare these lines when
two runs disagree:

```text
verbose: repository: /src/app at HEAD 4addf20f4554 (dirty, 2 changed paths)
verbose:   uncommitted linted file: .github/workflows/ci.yml
verbose: file: .github/workflows/ci.yml sha256:734e62c28dc1
verbose: config: .github/actionlint.yaml for project /src/app
verbose:   config file: .github/actionlint.yaml sha256:0f4983a108b9
verbose:   setting: /lint/rules/security/unpinned-reference/level = "error" (.github/actionlint.yaml:4)
verbose: ignore: --ignore unpinned-reference
verbose: ignore: config rule=shellcheck file=.github/workflows/ci.yml (.github/actionlint.yaml:6)
verbose: ignore: inline script-injection (.github/workflows/ci.yml:9)
verbose: plugin: github.com/kjanat/actionlint-plugin-shellcheck v0.3.1 sha256:6c5ec0746a00 active
verbose: tool: ruff inactive (disabled in configuration)
verbose: network: online, GitHub token from GH_TOKEN, cache /home/me/.cache/actionlint
verbose: network requests: 2 attempts, 0 failed; metadata cache: 37 hits, 2 misses; resolver cache hits: 0
verbose: remote metadata: 39 available
verbose: flags: strict=true experimental=true no-ignore=false; rules run: 41
verbose: rule script-injection: 1 collected, 0 reported
verbose: rule unpinned-reference: 2 collected, 0 reported
```

`--offline` blocks network requests for every command. Download dependencies before you disconnect:

```sh
actionlint cache warm .github/workflows
actionlint check --offline .github/workflows
```

`cache warm` downloads plugins and remote action metadata and prints the cache directory to save in
CI.

```sh
actionlint cache dir
actionlint cache list --json
actionlint cache prune --older-than 168h --dry-run
actionlint cache prune --older-than 168h
actionlint cache clean --dry-run
actionlint cache clean
```

`cache prune` removes downloads older than 30 days by default. `cache clean` removes everything
except plugin revocation records.

Offline checks warn about missing dependencies. A missing required config or plugin produces a
warning and exit status 0.
See [remote action metadata](remote-actions.md) for lookup order and resolver configuration.

### Network accounting

`--network-stats` writes grouped network statistics as a final JSON record on stderr:

```sh
actionlint check --network-stats --output=json .github
```

Each group names the host, request kind, purpose and priority. `credentials` is `provided`,
`absent` or `unknown`. The field does not show whether the server accepted them. Reports exclude
credential values, request paths and query strings.

`attempts` counts transport attempts, including retries and redirects. `failures` counts transport
errors and HTTP error responses. `quota_rejections` counts rejections before transport for
observed limits or reservations.

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

Set `all_requests_reported` only when the report includes every attempt. Supported kinds are
`http`, `rest`, `graphql`, `git` and `oci`. Without the field, actionlint counts the resolver's
calls under `unreported_resolver_calls`.

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

Repository discovery looks for `.github/actionlint.yaml`, `.github/actionlint.yml`,
`.github/actionlint.json`, `.github/actionlint.jsonc`, then `.github/actionlint.toml`.
`--no-config` skips project configuration but still loads
[shared network policies](config.md#shared-defaults-and-restrictions).

`config show --origin` adds the source of each value. `config validate` runs the same validation as
analysis and skips the stricter JSON Schema checks. `config schema` includes tool providers from
plugins.

`config init` creates `.github/actionlint.yaml`. `--json`, `--jsonc`, `--toml` or `--yml` selects
another format. The command refuses to run when a config file exists.

`rules` lists checks, and `rules NAME` explains one. `doctor` reports the selected config and
resolves tool commands without running them.

### Runner support

`runner-support` warns when a runner is deprecated or unsupported on the selected service. A
GitHub.com retirement does not apply to GHES.

In GitHub Actions, actionlint reads the service from `GITHUB_SERVER_URL`. For local checks, set
`repository.service` in configuration or pass it:

```sh
actionlint check --runner-service github.com workflow.yml
actionlint check --runner-service ghes --runner-service-version 3.19 workflow.yml
```

`--as-of YYYY-MM-DD` evaluates lifecycle events on a fixed UTC date. Unknown support data never
produces a violation. See [dated runner support](plugins.md#dated-runner-support).

### Lifecycle notification policies

Named policies decide when a known support deadline produces information, a warning or an error.

```toml
[repository]
service = "github.com"

[[lint.lifecycle.policies]]
id          = "prepare-for-support-removal"
description = "Give teams time to migrate before support ends."

[lint.lifecycle.policies.match]
subjects = ["runners", "action-runtimes", "language-runtimes"]
events   = ["support-ends"]
services = ["github.com", "github-data-residency"]

[[lint.lifecycle.policies.notifications]]
starts   = { before = "180d" }
severity = "info"

[[lint.lifecycle.policies.notifications]]
starts   = { before = "90d" }
severity = "warning"

[[lint.lifecycle.policies.notifications]]
starts   = { on = "effective-date" }
severity = "error"
```

With this policy, notifications start 180 UTC days before the deadline. They become warnings at 90
days and errors on the date itself. Boundaries are inclusive.

The events are `support-ends`, `support-starts` and `runtime-replaced`. `before` takes whole days
such as `30d`. `on` accepts `effective-date`. The highest severity wins when several policies match.

A policy with the same ID replaces the inherited one through `extends`. File overrides can require
an earlier migration for some workflows:

```toml
[[overrides]]
includes = [".github/workflows/release.yml"]

[[overrides.lint.lifecycle.policies]]
id          = "release-workflows-must-migrate-early"
description = "Require release workflows to migrate at least 30 days early."

[overrides.lint.lifecycle.policies.match]
subjects = ["runners", "action-runtimes", "language-runtimes"]
events   = ["support-ends"]

[[overrides.lint.lifecycle.policies.notifications]]
starts   = { before = "30d" }
severity = "error"
```

An explicit rule severity such as `runner-support = "error"` applies to every optional
notification from that rule. `off` disables them.

### Required lifecycle policies in CI

A CI administrator can require policies that project settings cannot weaken:

```sh
actionlint check --enforce-policy https://example.org/actionlint/required.toml
```

The file accepts only `lint.lifecycle.policies` and `extends`. Repeat `--enforce-policy` for several
files. A project may raise a required severity but cannot lower or suppress it.

Cache required policies before an offline check:

```sh
actionlint cache warm --enforce-policy https://example.org/actionlint/required.toml
actionlint check --offline --enforce-policy https://example.org/actionlint/required.toml
```

A missing or invalid required policy is an error.

### Shell completion

`completion <shell>` prints a completion script for `bash`, `fish`, `powershell` or `zsh`. `auto`
detects the shell from `$SHELL`. Bash needs `bash-completion`, and Zsh needs `compinit`. Regenerate
saved scripts after upgrading.

```sh
mkdir -p ~/.local/share/bash-completion/completions
actionlint completion bash >~/.local/share/bash-completion/completions/actionlint
```

```sh
mkdir -p ~/.config/fish/completions
actionlint completion fish >~/.config/fish/completions/actionlint.fish
```

Load the PowerShell script from your profile:

```powershell
New-Item -ItemType Directory -Force (Split-Path -Parent $PROFILE) | Out-Null
actionlint completion powershell | Out-File -Append -Encoding utf8 $PROFILE
```

To load it into the current session only:

```powershell
actionlint completion powershell | Out-String | Invoke-Expression
```

### Ignore some errors

Every rule supports [inline suppression comments](config.md#inline-diagnostic-suppressions) with a
rule name and an optional reason. Put the comment on the reported line, or use
`actionlint:ignore-next-line` on the line before. A directive on a YAML block scalar header covers
its body.

```yaml
cache-mode: write # actionlint:ignore cache-write-untrusted -- this job runs reviewed default-branch code only
```

`unused-suppression` reports selectors that match no findings. `actionlint fix --apply` removes
them.

Local actions need their `action.yml` or `action.yaml` at lint time. For generated actions,
[map the runtime path to its source](config.md#local-action-source-mappings) or suppress the
finding:

```yaml
- uses: ./generated-action # actionlint:ignore missing-local-action
```

[Reusable ignores](config.md#reusable-ignores) declare an exception once for matching files, jobs,
steps or action references.

`--ignore` filters findings by rule name or diagnostic code. Use `rule/code` to limit a code to one
rule. Ignored findings do not affect the exit status.

```sh
actionlint check --ignore missing-permissions --ignore SC2086 workflow.yml
actionlint check --ignore shellcheck/SC2086 workflow.yml
```

`--ignore-regex` filters by an [RE2][re2] pattern. The pattern matches the message and
`CODE: message`.

```sh
actionlint --ignore-regex 'SC2086:' --ignore-regex 'label ".+" is unknown'
```

`--tool ID` runs a tool with its command on `PATH`. `--tool ID=COMMAND` sets the command.
`--no-tool ID` disables one tool, and `--no-tools` disables every external linter.

```sh
actionlint --no-tool shellcheck
actionlint --no-tools
actionlint --tool shellcheck='shellcheck -e SC2086'
```

ShellCheck and Ruff run as registry Wasm plugins by default. The first online check downloads them.
When no published release of a default plugin loads in this actionlint, the check skips that plugin
and prints a warning.
Use `--tool shellcheck` or `--tool ruff` to run an installed native executable. See
[Ruff configuration](config.md#ruff) for Python checks.

The v1 options `-shellcheck` and `-pyflakes` print deprecation warnings. `-pyflakes` never runs its
command and selects Ruff's `F` rules.

Repository configuration can select native executables through `tools.<name>.backend.process`.
Pass `--allow-config-processes` only when you trust the whole configuration, including inherited
files. The `ACTIONLINT_TOOL_<ID>_*` variables configure
[executables, arguments and environments](env.md#external-linters).

ShellCheck runs in the step's working directory. Write file paths in the `--tool shellcheck` command as absolute paths.
actionlint sets ShellCheck's output format and input. Do not pass `-f`, `--format` or file
arguments.

actionlint passes `--norc` by default. Set [`tools.shellcheck.config`](config.md#shellcheck) to an
inline mapping or an rc file.

<a id="format"></a>

### Format error messages

Use `-format` to format error messages with [Go template syntax][go-template].

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

#### Formatting syntax

In a template, `.` is a sequence of error objects. Iterate over it with `range`:

```text
{{range $err := .}} this part iterates error objects with the iteration variable $err {{end}}
```

The error object has these fields.

<!-- dprint-ignore-start -->

| Field                | Description                                           | Example                                                             |
| -------------------- | ----------------------------------------------------- | ------------------------------------------------------------------- |
| `{{$err.Snippet}}`   | Code snippet to indicate error position               |  <code>          node_version: 16.x\n          ^~~~~~~~~~~~~</code> |
| `{{$err.Kind}}`      | Name of rule the error belongs to                     | `expression`                                                        |
| `{{$err.Code}}`      | Native diagnostic identifier, when supplied           | `SC2086`                                                            |
| `{{$err.Severity}}`  | Diagnostic level; empty means error                   | `info`                                                              |
| `{{$err.Filepath}}`  | Canonical relative file path of the error position    | `.github/workflows/ci.yml`                                          |
| `{{$err.Line}}`      | Line number of the error position (1-based)           | `9`                                                                 |
| `{{$err.Column}}`    | Column number of the error's start position (1-based) | `11`                                                                |
| `{{$err.EndColumn}}` | Column number of the error's end position (1-based)   | `23`                                                                |

<!-- dprint-ignore-end -->

actionlint adds these template actions.

| Action             | Description                                                                      | Example usage                             |
| ------------------ | -------------------------------------------------------------------------------- | ----------------------------------------- |
| `json x`           | Serialize `x` as JSON string followed by newline character                       | `{{json $err}}`                           |
| `escapeData x`     | Escape string `x` for a GitHub Actions command body (`%`, CR, LF)                | `{{$err.Snippet \| escapeData}}`          |
| `escapeProperty x` | Escape string `x` for a GitHub Actions command property (also `:` and `,`)       | `{{$err.Filepath \| escapeProperty}}`     |
| `toPascalCase x`   | Convert `x` into PascalCase (e.g. 'foo-bar' to 'FooBar')                         | `{{toPascalCase $err.Kind}}`              |
| `allKinds`         | Return an array of kind objects. The kind object is explained in the below table | `{{range $ = allKinds}}{{$.Name}}{{end}}` |
| `getVersion`       | Return the version of actionlint as string                                       | `{{getVersion}}`                          |

`allKinds` returns kind objects with these fields.

| Field            | Description      | Example        |
| ---------------- | ---------------- | -------------- |
| `{{$kind.Name}}` | Name of the kind | `syntax-check` |

actionlint unescapes backslash escapes such as `\n` in the format string.

### Exit status

| Exit status | Meaning                                                                                                   |
| ----------- | --------------------------------------------------------------------------------------------------------- |
| `0`         | Completed without findings that affect the exit status, or completed an information/configuration command |
| `1`         | Completed with findings that affect the exit status                                                       |
| `2`         | Invalid CLI arguments or incompatible options                                                             |
| `3`         | Could not initialize or complete, including invalid config, regex or template                             |

Errors fail a check. With `--strict`, warnings also fail it. Informational findings never do.
Dependency findings fail a check only when [`lint.dependencies`](config.md#findings-in-dependencies)
selects them. Known execution errors in downloaded actions always count.

<a id="on-github-actions"></a>

## Use actionlint on GitHub Actions

See the [Action guide](action.md).

### Manual installation

The download script sets the `executable` output to the downloaded binary's path. Set
`shell: bash` on each step. Windows runners default to `pwsh`.

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

To download and run in one step:

```yaml
- name: Check workflow files
  run: |
    bash <(curl -fsSL https://raw.githubusercontent.com/kjanat/actionlint/662318dd6bbd9c0c120e35b03168bc1be69bf428/scripts/download-actionlint.bash) 2.0.0
    ./actionlint --output github
  shell: bash
```

The script takes a version and a download directory.

Or use the [Docker image](#docker):

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

<a id="docker"></a>

## [Docker][docker] image

[![Docker Image Version][docker-badge]][dockerhub]

[Docker images][docker-image] built from this revision contain `actionlint`, ShellCheck and the
native Ruff executable.

The `latest` tag follows the latest stable release. A version tag such as
`ghcr.io/kjanat/actionlint:2.0.0` pins one release. Docker Hub carries the same images as
`kjanat/actionlint`. For byte-for-byte reproducibility, pin the manifest digest as
`ghcr.io/kjanat/actionlint:{version}@sha256:<digest>`.

```sh
docker run --rm ghcr.io/kjanat/actionlint:latest -version
```

To check every workflow in your repository, mount it at `/w`:

```sh
docker run --rm -v "$(git rev-parse --show-toplevel):/w" ghcr.io/kjanat/actionlint:latest -color
```

To check one file, pass its contents through stdin and use `-`:

```sh
cat /path/to/workflow.yml | docker run --rm -i ghcr.io/kjanat/actionlint:latest -color -
```

Or mount the workflows directory and pass the paths:

```sh
docker run --rm -v /path/to/workflows:/workflows ghcr.io/kjanat/actionlint:latest -color /workflows/ci.yml
```

Pass `SHELLCHECK_OPTS` into the container with `-e`:

```sh
docker run --rm -v "$(git rev-parse --show-toplevel):/w" \
  -e SHELLCHECK_OPTS='-e SC2086' ghcr.io/kjanat/actionlint:latest -color
```

<a id="tools-integ"></a>

## Tools integration

### reviewdog

The [`reviewdog/action-actionlint` action][reviewdog-actionlint] posts [reviewdog][reviewdog]
comments on changed lines and runs this fork with ShellCheck. Use v1.76.2 or newer.

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

Set `filter_mode: diff_context` to include surrounding lines, or `fail_level: none` to comment
without failing the step. Pass actionlint options through `actionlint_flags`.

<a id="problem-matchers"></a>

### GitHub annotations

The [GitHub Action](action.md) emits annotations by default. With an installed CLI, select GitHub
output:

```yaml
- name: Check workflow files
  run: actionlint check --output github
```

### Native Git hooks

[Git 2.54 and newer](https://git-scm.com/docs/git-hook/2.54.0) registers named hooks in its
configuration. [Install actionlint](install.md) on your `PATH`, then run:

```sh
git config set --local hook.actionlint.command 'actionlint pre-commit'
git config set --local hook.actionlint.event pre-commit
```

`actionlint pre-commit` checks discovered workflows and actions in the working tree, including
unstaged edits. It also works for `pre-push`.

Try the hooks before committing:

```sh
git hook list --show-scope pre-commit
git hook run pre-commit
```

To disable the hook and keep its configuration:

```sh
git config set --local hook.actionlint.enabled false
```

### pre-commit

v2 [pre-commit][pre-commit] hooks live in
[kjanat/actionlint-pre-commit](https://github.com/kjanat/actionlint-pre-commit). Replace the placeholder with a published tag:

```yaml
repos:
  - repo: https://github.com/kjanat/actionlint-pre-commit
    rev: <published-v2-tag>
    hooks:
      - id: actionlint
```

The `actionlint` hook installs the `actionlint-kjanat` wheel with the native binary. Every hook checks all repository workflows on every commit.

To migrate from v1, change the `repo:` URL. `pre-commit autoupdate` keeps the old repository. See
[v2 migration](migration-v2.md). Update with:

```sh
pre-commit autoupdate --freeze --repo https://github.com/kjanat/actionlint-pre-commit
```

### VS Code

The [Linter extension][vsc-extension] for [VS Code][vscode] runs the command in
`linter-actionlint.config` and reports errors as you edit workflows.

### Emacs

[MELPA][emacs-melpa] has [flycheck-actionlint][emacs-flycheck-extension] for
[Flycheck][emacs-flycheck] and [flymake-actionlint][emacs-flymake-extension] for
[Flymake][emacs-flymake]. Set `flycheck-actionlint-executable` or `flymake-actionlint-executable`
to use this fork.

### Vim and Neovim

[nvim-lint][nvim-lint] and [ALE][vim-ale] run actionlint as you edit workflows. Override
nvim-lint's command or set `g:ale_yaml_actionlint_executable` to use this fork.

### Pulsar Edit

The [Linter package][pulsar-linter] for [Pulsar Edit][pulsar] runs the executable at
`actionsExecutablePath`.

### Nova

[Actionlint for Nova][nova-extension] shows inline feedback in [Nova.app][nova]. Point
`actionlint.binarypath` at this fork's binary.

## Editor, fixes and remote resolution

`actionlint lsp` serves the Language Server Protocol over stdin and stdout. The client applies
quick-fix edits, and the server never writes editor buffers to disk.

The quick-fix menu offers **Suppress RULE for this declaration** for allowed diagnostics. The
action adds a suppression comment or extends an existing one. Rules under a
`forbidden-suppression` prohibition do not offer it.

`actionlint fix [files...]` previews source edits as JSON. `--apply` writes them and checks the
files again. `--include-suggested` adds edits that need review. Conflicting edits or stale sources
abort before any write.

`actionlint plugin search` finds published plugins. `plugin install owner/repo@TAG` adds an exact
version to the config's `plugins` list. Commit that file. `plugin sync` restores exact pins, and
`plugin update` refreshes declared versions. See
[plugin installation](plugins.md#install-from-the-registry).

`plugin versions owner/repo` marks each published tag `compatible` or `incompatible` with this
actionlint. An incompatible tag shows the interface that this actionlint cannot load. A tag is
`unknown` when the registry lists no imports and exports for it. `plugin install` and
`plugin update` without an exact tag select the highest release that this actionlint can load.
Install, update and sync read each downloaded WebAssembly component and fail on a component that
this actionlint cannot load. Install and update then leave the config unchanged.

`--plugin PATH` loads a WebAssembly plugin or a replacement resolver component. A `.json` path
loads a [local tool provider manifest](plugins.md#local-tool-provider-manifests) that runs a
native command.

`--resolve-with EXECUTABLE` selects a native resolver and enables commit-pin suggestions. Repeat
`--resolver-arg VALUE` to pass literal arguments. See
[remote metadata and reference resolution](remote-actions.md).

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
[go-template]: https://pkg.go.dev/text/template
[nova-extension]: https://extensions.panic.com/extensions/org.netwrk/org.netwrk.actionlint/
[nova]: https://nova.app
[nvim-lint]: https://github.com/mfussenegger/nvim-lint
[pre-commit]: https://pre-commit.com
[pulsar-linter]: https://web.pulsar-edit.dev/packages/linter-github-actions
[pulsar]: https://pulsar-edit.dev/
[re2]: https://golang.org/s/re2syntax
[reviewdog-actionlint]: https://github.com/reviewdog/action-actionlint
[reviewdog]: https://github.com/reviewdog/reviewdog
[vim-ale]: https://github.com/dense-analysis/ale
[vsc-extension]: https://marketplace.visualstudio.com/items?itemName=arahata.linter-actionlint
[vscode]: https://code.visualstudio.com/
