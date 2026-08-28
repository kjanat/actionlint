# Usage

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

### Shell completion

`-completion` prints a completion script for the given shell to stdout. The
supported shells are `bash`, `fish`, `powershell` and `zsh`. The value also
accepts `pwsh` as an alias for `powershell`, a shell path so that
`actionlint -completion "$SHELL"` works, and `auto` to pick the shell from
`$SHELL`, falling back to PowerShell when `$PSModulePath` is set. The script
completes the flags in both their `-flag` and `--flag` spellings, the values
they take, and workflow file paths.

```sh
mkdir -p ~/.local/share/bash-completion/completions
actionlint -completion bash > ~/.local/share/bash-completion/completions/actionlint
```

```sh
mkdir -p ~/.config/fish/completions
actionlint -completion fish > ~/.config/fish/completions/actionlint.fish
```

The zsh script belongs in a directory listed in `$fpath`.

```sh
actionlint -completion zsh > "${fpath[1]}/_actionlint"
```

The PowerShell script is loaded from your profile. The first command creates
the profile's directory, which does not exist on a fresh account and makes
`Out-File` fail.

```powershell
New-Item -ItemType Directory -Force (Split-Path -Parent $PROFILE) | Out-Null
actionlint -completion powershell | Out-File -Append -Encoding utf8 $PROFILE
```

To load the script into the current session only, pipe it through
`Invoke-Expression` instead.

```powershell
actionlint -completion powershell | Out-String | Invoke-Expression
```

### Ignore some errors

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

Your arguments are prepended to the ones actionlint appends itself, so do not
pass `-f`/`--format` or file arguments. actionlint appends
`--norc -f json1 -x --shell <sh> -e SC1091,SC2194,SC2050,SC2153,SC2154,SC2157,SC2043 -`
and parses the JSON1 output.

Because of that `--norc`, a repository `.shellcheckrc` is not read. Use
`-shellcheck '<command line>'` or the [`SHELLCHECK_OPTS` environment variable](checks.md#check-shellcheck-integ) instead. pyflakes has no
configuration file and no `# noqa`, so suppress its findings with `-ignore` or
the `paths:` section of the configuration file.

<a id="format"></a>

### Format error messages

`-format` option can flexibly format error messages with [Go template syntax][go-template].

Before explaining the formatting details, let's see some examples.

#### Example: Serialized into JSON

```sh
actionlint -format '{{json .}}'
```

Output:

```json
[{"message":"unexpected key \"branch\" for ...
```

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

````markdown
### Error at line 21, col 20 of `test.yaml`

property "platform" is not defined in object type {os: string}

<!-- dprint-ignore-start -->

```plaintext
          key: ${{ matrix.platform }}-node-${{ hashFiles('**/package-lock.json') }}
                   ^~~~~~~~~~~~~~~
```
````

<!-- dprint-ignore-end -->

#### Example: Serialized in [JSON Lines][jsonl]

```sh
actionlint -format '{{range $err := .}}{{json $err}}{{end}}'
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

<img src="https://github.com/rhysd/ss/blob/master/actionlint/ga-annotate.png?raw=true" alt="annotations on GitHub Actions" width="731" height="522"/>

To include newlines in the annotation body, it prints `%0A`. (ref
[actions/toolkit#193](https://github.com/actions/toolkit/issues/193)). And it
suppresses `SC2016` shellcheck rule error since it complains about the template
argument.

Basically it is more recommended to use [Problem Matchers](#problem-matchers) or
reviewdog as explained in ['Tools integration' section](#tools-integ) below.

#### Example: [SARIF format][sarif]

[The Static Analysis Results Interchange Format (SARIF)][sarif] is a standardized format for the results of static analysis tools.

Since this practical format is much more complex than the above examples, the template is not written here. Please read
[the template file in test data](../testdata/format/sarif_template.txt).

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
| `{{$err.Filepath}}`  | Canonical relative file path of the error position    | `.github/workflows/ci.yaml`                                         |
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

The repository provides a Docker action backed by a prebuilt image containing
actionlint, ShellCheck, and pyflakes. It reports each problem as a GitHub
annotation by default, without compiling actionlint in the consumer's workflow.

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

Docker actions require a Linux runner. `v1` moves to each new release.
`v1.13.0` is a versioned release tag, but only a full-length commit SHA provides
an immutable action reference.

The action accepts these inputs:

| Input               | Default       | Description                                                                  |
| ------------------- | ------------- | ---------------------------------------------------------------------------- |
| `files`             | all workflows | Newline-separated workflow file paths                                        |
| `format`            | `github`      | `github`, `default`, `oneline`, `json`, `json-lines`, `markdown`, or `sarif` |
| `ignore`            | none          | Newline-separated regular expressions for errors to ignore                   |
| `config-file`       | automatic     | Configuration file relative to `working-directory`                           |
| `shellcheck`        | `true`        | Enable ShellCheck integration                                                |
| `pyflakes`          | `true`        | Enable pyflakes integration                                                  |
| `working-directory` | `.`           | Directory to lint, relative to the repository workspace                      |
| `output-file`       | none          | Repository-relative file to receive the selected output                      |
| `fail-on-error`     | `true`        | Fail when problems are found; command failures always fail                   |

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
      .github/workflows/ci.yaml
      .github/workflows/release.yaml
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

The download script remains useful on macOS, Windows, or when direct access to
the executable is preferred. It sets an absolute file path of the downloaded
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
      - uses: actions/checkout@v6
        with: { persist-credentials: false }
      - name: Download actionlint
        id: get_actionlint
        run: bash <(curl -fsSL https://raw.githubusercontent.com/kjanat/actionlint/HEAD/scripts/download-actionlint.bash) 1.13.0
        shell: bash
      - name: Check workflow files
        run: ${{ steps.get_actionlint.outputs.executable }} -color
        shell: bash
```

Or simply download the executable and run it in one step:

```yaml
- name: Check workflow files
  run: |
    bash <(curl -fsSL https://raw.githubusercontent.com/kjanat/actionlint/HEAD/scripts/download-actionlint.bash) 1.13.0
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
      - uses: actions/checkout@v6
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

[Docker image][docker-image] is available. The image contains `actionlint`
executable and all dependencies (shellcheck and pyflakes).

Available tags are:

- `ghcr.io/kjanat/actionlint:latest`:\
  Moving alias for the latest stable version of actionlint. This image is recommended.
- `ghcr.io/kjanat/actionlint:{version}`:\
  Release-specific actionlint image rather than a moving alias.\
  (e.g. `ghcr.io/kjanat/actionlint:1.13.0`)
- `ghcr.io/kjanat/actionlint:action-{version}`:\
  Release-specific image used by `action.yml` rather than a moving alias.\
  (e.g. `action-1.13.0`)
- `ghcr.io/kjanat/actionlint:action-v1`:\
  Moving alias for the latest compatible v1 image available to Docker Action users.
- `ghcr.io/kjanat/actionlint:action-latest`:\
  Moving alias for the latest stable image available to Docker Action users.

The CLI image is also published to Docker Hub as `kjanat/actionlint:latest` and
`kjanat/actionlint:{version}`. Both registries carry the same manifest, so pick
whichever your setup pulls from more easily. The `action-*` tags exist on
`ghcr.io` only, since `action.yml` refers to them there.

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

> [!WARNING]
> `reviewdog/action-actionlint` uses a hard-coded installer for `rhysd/actionlint`.
> It does NOT use this fork.

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
      - uses: actions/checkout@v6
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
    bash <(curl -fsSL https://raw.githubusercontent.com/kjanat/actionlint/HEAD/scripts/download-actionlint.bash) 1.13.0
    ./actionlint -color
  shell: bash
```

When you change your workflow and the changed line causes a new error, CI will
annotate the diff with the extracted error message.

<img
  src="https://github.com/rhysd/ss/blob/master/actionlint/problem-matcher.png?raw=true"
  alt="annotation by Problem Matchers"
  width="715"
  height="221"
/>

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
[super-linter/super-linter#1852](https://github.com/super-linter/super-linter/issues/1852)
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
    rev: v1.13.0
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
reproducible pre-commit environment. A scheduled [version lifecycle workflow](../.github/workflows/shellcheck-versions.yaml) checks both go-shellcheck
and the ShellCheck version it embeds, and proposes pin updates automatically.
To choose a different version yourself, use `additional_dependencies` on the
plain hook:

```yaml
---
repos:
  - repo: https://github.com/kjanat/actionlint
    rev: v1.13.0
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
trunk check enable actionlint@1.13.0
```

or modify `.trunk/trunk.yaml` in your repository to contain:

```yaml
lint:
  enabled:
    - actionlint@1.13.0
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
