# Plugins

actionlint loads plugins as WebAssembly components, core Wasm script engines, or
[process-provider manifests](#local-tool-provider-manifests) for external tools.
The [TypeScript SDK](../plugins/sdk/typescript/README.md) builds, tests and packages manifests.
See [contribute a rule](#contribute-a-rule) to choose an interface.

## Install from the registry

```sh
actionlint plugin install shellcheck
actionlint check .github
```

### Install a preview or CDN artifact

Pass the manifest URL directly to actionlint:

```sh
actionlint plugin install 'https://esm.sh/pr/kjanat/actionlint/@kjanat-actionlint/plugin-yamllint@afaa8f8/actionlint-provider.json?raw'
actionlint check --allow-config-processes .github
```

actionlint validates the file, saves its SHA-256 in the config and caches it.

### Find published versions

Search the [published catalogue](https://plugins.actionlint.kjanat.dev/):

```sh
actionlint plugin search shellcheck
actionlint plugin install shellcheck
actionlint plugin versions github.com/owner/plugin
actionlint plugin install owner/plugin@v1.2.3
actionlint plugin list
actionlint check
```

Installation writes the root `plugins` list of your existing config file, or creates `.github/actionlint.yaml`:

```yaml
plugins:
  - github.com/owner/plugin@v1.2.3
  - gitlab.com/group/plugin@v1.2.3
```

Install resolves an alias and writes the canonical package and exact version to the config.
Without `@version`, or with `--version latest`, install selects the newest stable semantic version
that this actionlint can load. A plugin with only prereleases gets its newest loadable prerelease.
A release is loadable when its downloaded component passes the checks that loading runs.
`plugin versions` marks each tag `compatible`, `incompatible` or `unknown`.
Reinstalling keeps the declared version and registry.
Use an object for a custom registry:

```yaml
plugins:
  - name: github.com/owner/plugin
    version: v1.2.3
    registry: https://example.org/catalogue/
```

In a TOML config, installation appends plugin tables after the existing tables:

```toml
#:schema https://cdn.jsdelivr.net/gh/kjanat/actionlint@2/actionlint.schema.json

[lint.rules]
preset = "recommended"

[[plugins]]
name    = "github.com/kjanat/actionlint-plugin-shellcheck"
version = "v0.2.0"
```

A direct artifact table replaces `name`, `version` and `registry` with a digest-pinned `source` URL or OCI reference.

```sh
actionlint plugin update github.com/owner/plugin --version latest
actionlint plugin update
actionlint plugin sync
actionlint plugin remove github.com/owner/plugin
```

Run `sync` after cloning or in CI. It restores the pinned artifacts into the user cache and refreshes withdrawal status.
Checks also restore missing artifacts, and a populated cache works offline.
Linting never upgrades a plugin or rewrites project files.
`latest` refuses to downgrade. Name an exact tag to downgrade on purpose.

After editing `plugins` by hand, run `actionlint plugin install` without arguments.
`plugins: []` or `plugins: null` disables all plugins, including inherited ones.
A child config replaces an inherited list as a whole.

`actionlint config init` installs and pins `shellcheck` and `ruff`.
See [configuration migration](config-migration.md).

Without a `plugins` list, checks still use these two registry plugins. Ruff's Wasm downloads only when a Python script
needs it. Run `actionlint cache warm` to download both for offline use.
An explicit `--tool` command or native backend selects the native linter.

### Try an unpublished candidate

Use a local candidate catalogue to install a packaged plugin before creating a release or registry submission:

```sh
actionlint plugin install owner/plugin@v1.2.3 --registry file:///absolute/path/to/candidate/
actionlint check
```

On Windows, use `file:///C:/path/to/candidate/`.
A project keeps the imported bytes after you rebuild or delete the candidate.
Run `plugin update owner/plugin` to select rebuilt bytes.

### Deprecated, withdrawn and revoked releases

Deprecated versions install with a warning. New resolution skips withdrawn versions, and existing pins keep working with
a warning. actionlint refuses revoked digests, including cached bytes.

The language server applies a revocation before its next analysis without a restart.
Direct URLs have no withdrawal status.

### Pin a directly hosted artifact

An HTTPS URL can appear in the same list, with a mandatory SHA256 pin:

```yaml
plugins:
  - "https://example.org/plugins/check.wasm#sha256=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
```

Replace the example digest with the file's SHA256.

Plugin sources from repository configuration reach public addresses only, including through redirects.
An explicit `--plugin` source, `--registry` endpoint or inspect/install URL may name a private host for that invocation.
The saved declaration does not keep that permission. `--allow-config-processes` grants no private network access.

### Pull from an OCI registry

Publish a plugin or tool provider as an OCI artifact, for example to GHCR with `oras`. Declare it by digest:

```yaml
plugins:
  - "oci://ghcr.io/owner/provider@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
```

The digest names an image index or an image manifest. An index selects the manifest for this host's `GOOS/GOARCH`.
The image manifest has exactly one primary layer:

- An `application/wasm` layer is a WebAssembly module.
- An `application/vnd.actionlint.tool-provider.v1+json` layer is a `transport: "process"` tool provider manifest.
  It can embed program and jq source or reference other layers by digest.

Stage a built Wasm plugin in an OCI image layout with ORAS:

```sh
oras push --oci-layout ./plugin-layout:example --artifact-type application/wasm plugin.wasm:application/wasm
oras manifest fetch --oci-layout ./plugin-layout:example
```

The registry's candidate check accepts `--oci-layout` to test a local graph before upload. See the
[OCI publication checks](https://github.com/kjanat/actionlint-registry/blob/master/docs/publishing.md#oci-checks-before-upload).

`actionlint plugin install oci://ghcr.io/owner/provider:v1` resolves the tag once and saves its digest.
Every other command refuses a tag. actionlint pulls anonymously. Only an explicitly selected `localhost` or loopback
registry may use plain HTTP.

An OCI tool provider runs a native executable outside any sandbox. A provider from repository configuration runs only
with `--allow-config-processes`, which the GitHub Action never sets. A `--plugin` source needs no opt-in.
actionlint downloads the executable only after that check passes.
`plugin install` and `plugin sync` download every layer for this host, and analysis then runs offline.

### GitHub Action caching

The JavaScript Action loads the same declarations as the CLI. Its `plugin-cache` input defaults to `true` and restores
verified artifacts from the GitHub Actions cache. The Action saves the cache even when linting reports findings.
A cache service failure produces a warning. Set `plugin-cache: false` to disable the cache.

The Action never installs native Ruff or ShellCheck. A component that declares the ShellCheck integration uses
`tools.shellcheck` and the `shellcheck` rule settings.

## Load a plugin directly

```sh
actionlint --plugin ./pipe-to-shell.wasm --plugin ./no-sudo.wasm
```

Each `--plugin` loads one component or core script engine. Findings carry the plugin name as their rule.
actionlint's ignores apply to them like built-in rules. `--log-level debug` prints the plugin's limits and forwards its
log lines, stdout and stderr.

Check each repository with its own invocation. The language server reads the selection at startup. Restart it after
installing, updating or removing plugins.
Outside a Git repository, pass `--config FILE`.

Pass the same `--plugin` arguments to `config show`, `config validate`, `doctor` and `config schema` when the
configuration names plugin rules.

The host refuses a component that exports an unsupported interface version, imports an ungranted interface, exports
no supported role or declares an interface in its metadata that it does not export.

Run `actionlint rules --plugin PATH --json` to list a plugin's rules, or `actionlint rules RULE --plugin PATH` to
explain one. Duplicate rule names fail before analysis.

### Suppression directives

actionlint applies its own directives (`actionlint:ignore`, `actionlint-ignore`, `ignore[...]` and the next-line forms)
and configured `ignores` after the plugin returns findings. The plugin or wrapped linter handles native directives such
as `noqa` and `shellcheck disable`. Selectors use the rule ID registered with actionlint. Upstream check IDs are diagnostic codes.
Rule IDs cannot contain whitespace, commas or square brackets.

`forbidden-suppression` affects actionlint's ignores only. The LSP suppression action always adds an actionlint
directive.

## Limits and capabilities

Each analysis call runs in a fresh instance with a 64 MiB memory limit and a 10-second timeout.
A trap, panic, exit, exhausted limit or cancellation fails that document's analysis.
actionlint continues with the next document and exits with status 3.

Each call also has budgets of 262,144 values and 8 MiB of string and collection storage.
Stdout, stderr and `host.log` share 1 MiB of output per call.
Exceeding the output budget fails the analysis even when the plugin handles the write error.

The host refuses `wasi:sockets`, `wasi:http` and custom interfaces at load.

Standalone analyzers get no filesystem access. The managed ShellCheck adapter grants the workspace and configured
source directories.

## Runner providers

[`plugins/protocol/wit/runner.wit`](../plugins/protocol/wit/runner.wit) defines the `runner-plugin` world.
A provider exports `describe` and `resolve`. The host resolves each complete `runs-on` selector before checks run.
All labels in a selector must match the same runner. Matrix combinations resolve separately.

`--runner-target` defaults to `github`.
Pass another target explicitly, and choose among several providers with `--runner-provider`.

A `runner-label` finding needs a fresh, complete dataset. Stale or incomplete data leaves labels and default shells
unknown. Explicit shells and container defaults override provider defaults.

A configured self-hosted label with `windows` as a whole hyphen-separated segment, in any case, implies Windows and the
`pwsh` default. Contradictory OS labels produce a runner conflict. For other custom labels, use a runner provider with a
snapshot.

### Custom GitHub runner inventory

Use the built-in `snapshot` provider for arbitrary labels such as `desktop-pool`, without writing a plugin:

```sh
actionlint --runner-provider snapshot --runner-scope my-org/my-repo \
  --runner-snapshot runners.json workflow.yml
```

`runners.json` holds the runner inventory in `content`:

```json
{
  "scope": "my-org/my-repo",
  "obtained_at": "2026-10-23T00:00:00Z",
  "freshness": "fresh",
  "complete": true,
  "content": {
    "runners": [{
      "name": "desktop",
      "labels": ["self-hosted", "desktop-pool", "x64"],
      "group": "build",
      "os": "windows",
      "architecture": "x64",
      "default_shell": "pwsh",
      "case_sensitivity": "insensitive"
    }]
  }
}
```

Each runner needs a unique, nonempty `name`. Labels match case-insensitively, and `group` matches exactly.
All labels and the group must match one runner. List only runners eligible for the repository scope.

Optional facts are `hosting` (`hosted`, `self-hosted`), `os` (`linux`, `macos`, `windows`, `unknown`), `architecture`,
`default_shell`, `case_sensitivity` (`sensitive`, `insensitive`, `unknown`), `sh_is_dash` and `ephemeral`.

Unknown fields and invalid enum values are errors. An empty `runners` array proves that no runner exists only when the
snapshot is fresh and complete.

A snapshot alone leaves the default `github` provider in place. The `snapshot` provider adds no hosted labels or
configured allowlists.

With this inventory, `runs-on: desktop-pool` supplies Windows and `pwsh` to runner checks.
Load a script engine that advertises `capabilities.script_shells: ["pwsh", "powershell"]` to lint those snippets.
Step, job and workflow `shell` settings override the inventory, and a job container supplies its own `sh` default.

### Offline Buildkite example

[`plugins/examples/buildkite-runners/src/lib.rs`](../plugins/examples/buildkite-runners/src/lib.rs) resolves an
offline queue snapshot. Its [README](../plugins/examples/buildkite-runners/README.md) covers tests and packaging.

```sh
cargo build --locked --profile wasm --target wasm32-wasip2 -p actionlint-example-buildkite-runners
actionlint --plugin target/wasm32-wasip2/wasm/buildkite_runners.wasm \
  --runner-target buildkite --runner-provider buildkite-runners \
  --runner-scope my-org/my-repo --runner-snapshot runners.json workflow.yml
```

The snapshot uses the same envelope with provider-specific `content`:

```json
{
  "scope": "my-org/my-repo",
  "obtained_at": "2026-10-19T00:00:00Z",
  "freshness": "fresh",
  "complete": true,
  "content": {
    "queues": [{
      "name": "windows-build",
      "labels": ["buildkite-windows", "x64"],
      "group": "build",
      "os": "windows",
      "architecture": "x64",
      "shell": "pwsh"
    }]
  }
}
```

The host reads the snapshot once per session. Replace it before the next CLI run, or restart the language server.
actionlint never expires or refreshes a snapshot.

### Dated runner support

The `runner-support` rule warns when a matching runner's provider reports that execution is no longer supported.
Unknown, stale, incomplete or mixed data produces no warning.

Declare the service that executes the workflows when the host cannot supply it:

```toml
[repository]
service = "github.com"
```

For GHES, use `service = "ghes"` or `service = {kind = "ghes", version = "3.19"}`. GitHub Enterprise Cloud with data
residency uses `service = "github-data-residency"`. `null` clears an inherited value. `--runner-service` and
`--runner-service-version` override configuration. Clone URLs do not set the service.

`ubuntu-24.04-arm` and `windows-11-arm` are ARM64 runners and stay valid.

## Write a plugin

`describe` returns the plugin name and its required facts.
`analyze` receives the document, its scripts, scripts from invoked composite actions and the registered JSON options.
It returns diagnostics and an optional error. An incomplete outcome can still carry diagnostics.

A component implements one or more of these interfaces:

- `kjanat:actionlint/analyzer@0.2.0`, defined in [`analyzer.wit`](../plugins/protocol/wit/analyzer.wit), checks one document per call.
- `kjanat:actionlint/runner-provider`, version 0.2.0 or 0.3.0, resolves `runs-on` labels.
- `kjanat:actionlint/script-engine`, version [0.2.0](../plugins/protocol/wit/script-engine.wit) or
  [0.3.0](../plugins/protocol/wit/deps/actionlint-0.3.0/script-engine.wit), checks script snippets.
- `kjanat:actionlint-resolver/reference-resolver@0.1.0` resolves repository references. See [reference resolution](remote-actions.md#reference-resolution).

The [`template-effects`](../plugins/protocol/wit/template-effects.wit) export marks diagnostics that depend on masked
workflow expressions, and the host filters them. Check operation 2 of the
[core JSON ABI](../plugins/protocol/wit/core-json-abi.md) carries the same marks.

Script spans map each line back to the declaring document. A diagnostic `rule` must be empty or equal the plugin name.
Put check identifiers in `code`. An empty `path` means the analyzed document.

Select the `annotated-plugin` world to import [`source-annotations`](../plugins/protocol/wit/source-annotations.wit).
Its `comments` function returns the YAML comments attached before a script declaration or after its scalar header.
Each plugin interprets its own comment syntax. The ShellCheck plugin treats these placements as script-wide directives:

```yaml
- name: Leading YAML directive
  # shellcheck disable=SC2086
  run: echo $HI
- run: echo $HI # shellcheck disable=SC2086
- run: | # shellcheck disable=SC2086
    echo $HI
```

Directives inside `run: |` keep ShellCheck's normal scope.

### Rust

[`plugins/examples/pipe-to-shell/src/lib.rs`](../plugins/examples/pipe-to-shell/src/lib.rs) flags `curl ... | sh`
patterns. It depends on [`actionlint-extension-api`](../plugins/sdk/rust). The SDK's
[README](../plugins/sdk/rust/README.md) contains a minimal analyzer. Build the example:

```sh
cargo build --locked --profile wasm --target wasm32-wasip2 -p actionlint-example-pipe-to-shell
```

The component is `target/wasm32-wasip2/wasm/pipe_to_shell.wasm` at the repository root.

Enable the SDK's `source-annotations` feature to import YAML comments. See the
[source-annotations example](../plugins/examples/source-annotations/README.md).
The SDK's native `package` feature writes discovery metadata, schema and license sidecars, and checksums for a
component.

[`plugins/examples/pinned-actions/src/lib.rs`](../plugins/examples/pinned-actions/src/lib.rs) requests only
`dependencies`. It flags remote action and reusable-workflow references without a full commit hash. It reads the host's
parsed `uses` values and never reparses the workflow.

```sh
cargo build --locked --profile wasm --target wasm32-wasip2 -p actionlint-example-pinned-actions
```

The component is `target/wasm32-wasip2/wasm/pinned_actions.wasm`.

### C

[`plugins/examples/no-sudo`](../plugins/examples/no-sudo) flags `sudo` at the start of a command. `build.sh` generates bindings with `wit-bindgen c` and compiles with wasi-sdk:

```sh
WASI_SDK_PATH=/opt/wasi-sdk ./build.sh no-sudo.wasm
```

### TinyGo

[`plugins/examples/insecure-download`](../plugins/examples/insecure-download) flags `curl -k` and
`wget --no-check-certificate` with the Go SDK's guest bindings. From the repository root, run:

```sh
mise exec -- bash plugins/examples/insecure-download/build.sh
```

The [example README](../plugins/examples/insecure-download/README.md) covers tests and release packaging.

### Build the examples

Run `mise run plugin-examples` from the repository root to build the Rust, C and TinyGo examples.

Other languages can vendor the
[`plugins/protocol/wit`](../plugins/protocol/wit) contracts and generate bindings from that copy.

## Source positions and fixes

Analyzer WIT positions use one-based Unicode character columns and exclusive ends.

Analyzer WIT fixes carry a digest for each edited document. actionlint checks recorded revisions and ranges against
current files before applying a fix.

WIT 0.2 related locations can point only into the analyzed document. actionlint checks every related location against
the captured text. It drops failed locations and reports their count in an `admission-rejected` error, which marks the
analysis incomplete.

## Script engines

actionlint selects Python and shell snippets, masks workflow expressions and maps positions back.
The engine checks one snippet at a time. Components implement
[`script-engine.wit`](../plugins/protocol/wit/script-engine.wit), world `script-engine-plugin`, with `describe`,
`validate-config` and `check`. Coordinates count one-based Unicode scalar values with exclusive ends.

A component exports exactly one of these versions:

- `kjanat:actionlint/script-engine@0.2.0` returns diagnostics without fixes and reads no files.
  It pairs with [`template-effects@0.2.0`](../plugins/protocol/wit/template-effects.wit).
- `kjanat:actionlint/script-engine@0.3.0` ([`0.3.0/script-engine.wit`](../plugins/protocol/wit/deps/actionlint-0.3.0/script-engine.wit))
  declares `fixes`, `reported-sources` and `resources` capabilities. The request adds `excluded` diagnostic codes such
  as `SC1091` and `directory`. Diagnostics add `fixes` in snippet coordinates, a `source` for findings in other files,
  and the engine's own severity string. It pairs with
  [`template-effects@0.3.0`](../plugins/protocol/wit/deps/actionlint-0.3.0/template-effects.wit).

The only resource kind is `directory`. The host mounts the script's working directory read-only at `/directory` for
that call. A directory outside the workspace, including through a symlink, fails the check. A missing directory gets no
access. A `source` path is relative and slash-separated below that directory, without `..`, backslashes or colons, and
cannot carry fixes. The host rejects fixes and reported sources that the engine did not declare.

[actionlint-plugin-ruff](https://github.com/kjanat/actionlint-plugin-ruff) supplies a component and a legacy core
module. Select either artifact through `--plugin`, the registry, or the tool configuration:

```yaml
tools:
  ruff:
    backend:
      wasm:
        path: ./tools/plugin.component.wasm.gz
```

Core modules use the version 1 [JSON ABI](../plugins/protocol/wit/core-json-abi.md). Check version 3 matches
script-engine 0.3.0. Each call runs in a pooled instance with a 256 MiB memory
limit and a 10-second timeout. Every response must fit in 8 MiB, and defined tables need finite maxima. Native
compilation has its own one-minute deadline. A trap or exceeded limit fails that document's check.

Script engines can also declare action-input sites in static metadata:

```json
{
  "capabilities": {
    "script_shells": [],
    "script_inputs": [
      {
        "action": "actions/github-script",
        "input": "script",
        "language": "javascript"
      }
    ]
  }
}
```

A selector has a lowercase owner/repository, an optional case-sensitive subpath without `@ref`, an input name and a
language. Selectors match exactly. The host rejects wildcards, duplicates, conflicting languages and malformed
selectors. An engine allows up to 256 selectors.

Input requests use the virtual filename
`actionlint-input/<language>/<owner>/<repository>[/<subpath>]/<input>.<extension>`, with `py`, `js` and `ts` for
Python, JavaScript and TypeScript.
Input-only engines receive no `run` scripts. A shell directive cannot opt action inputs into ShellCheck.
Engines that replace Ruff or ShellCheck read `tools.ruff` or `tools.shellcheck` for inputs too.

### Local tool provider manifests

Any native command that checks a snippet on stdin can run as a script tool.
Describe the command in a tool provider manifest and pass it with `--plugin`:

```sh
actionlint --plugin ./tools/mylint/manifest.json
```

The manifest has `schema_version: 1` and `transport: "process"`. `rule.name` is the tool ID for `tools.<id>` and
`lint.rules`. `selectors.shells`, `selectors.inputs` and `selectors.filename` choose the scripts.

- `process.entry` is a command on `PATH`. A local manifest declares no `artifacts`.
- `process.stdin` is `source` (the masked snippet) or `request` (the JSON check request).
- The command runs with a scrubbed environment. Only `process.env` and the names in `process.env_passthrough` reach it.
  List `PATH`, `HOME` or `TEMP` there when the command needs them.
- `process.exit_codes` lists accepted statuses, default `[0]`. A nonzero status needs nonempty stdout.
- `process.output.format` is `script-diagnostics@1`, `sarif@2.1.0`, or `json`/`text` with a jq transform.
  `process.output.coordinates` declares the tool's row, column and end conventions when they differ from LF rows,
  character columns and exclusive ends.
- A transform has a `digest` (`sha256:<hex>` of a `.jq` file beside the manifest) or an `inline` body. It sees stdout
  as `.` and the arguments plus `exit_code` as `$vars`. It returns a diagnostics array or `{diagnostics, sources, errors}`.
- `capabilities.message_template` formats messages from `{code}`, `{severity}`, `{row}`, `{column}` and `{message}`.
  `capabilities.fixes` and `capabilities.reported_sources` allow suggested edits and findings in other files.

Inline `tools.<id>.config` settings reach the tool through `process.config.render`, after an optional
`process.config.transform`. The manifest replaces a bundled tool with the same rule name.

`configuration_transform` is a jq program that normalizes settings for a process or a component. The host validates the original settings against `config_schema` first.
`process.config_schema` validates the converted options. List path settings in `configuration_paths` as JSON pointers,
such as `["/source-path"]`, to expand configuration variables in them.

Use `preparation_transform` when tool options depend on the script or its interpreter. It receives:

- `source`, `language` and `startup` hold the original script, inferred interpreter and its startup commands.
- `environment_startup` and `environment_known` hold declarations for known environment variable names and whether those names are fully known.
- `settings` holds the result of `configuration_transform`, including `options`.
- `config_file`, `filesystem`, `engine` and `input` hold the resolved configuration file, filesystem availability, whether execution uses a snippet engine, and whether the script comes from an action input.

It returns `source_prefix` for generated comments, `startup` for generated commands, `config` for tool options,
`variables` for argument substitutions and `excluded` for diagnostic codes. A nonempty `skip` skips the script.
Generated comments must end with a newline. The host inserts comments after a shebang and commands after leading
comments. Edits never touch the inserted lines.

The bundled ShellCheck [configuration](../internal/model/bundledproviders/shellcheck/configuration.jq) and
[preparation](../internal/model/bundledproviders/shellcheck/preparation.jq) programs are working examples.

### PSScriptAnalyzer example

The [PSScriptAnalyzer provider](../plugins/examples/psscriptanalyzer/provider.json) checks `shell: pwsh` and
`shell: powershell` snippets with PowerShell 7 and
[PSScriptAnalyzer](https://learn.microsoft.com/en-us/powershell/utility-modules/psscriptanalyzer/overview) 1.25.0 or
newer. Install the module yourself:

```powershell
Install-PSResource PSScriptAnalyzer -Version 1.25.0 -Scope CurrentUser
```

Load the provider alongside your configuration:

```sh
actionlint check --plugin ./plugins/examples/psscriptanalyzer/provider.json .github/workflows/ci.yml
```

Its configuration is PSScriptAnalyzer settings data:

```yaml
tools:
  psscriptanalyzer:
    config:
      IncludeRules: [PSAvoidUsingCmdletAliases, PSAvoidUsingInvokeExpression]
```

A missing module or crashed process fails the provider. The provider parses `powershell` snippets with PowerShell 7.
Configure PSScriptAnalyzer compatibility rules for older PowerShell language versions.

## Remote artifacts and static metadata

`--plugin` and `tools.<id>.backend.wasm.path` accept local paths, absolute `file://` URIs, HTTP(S) URLs and `oci://`
references. Both accept raw and gzip-compressed Wasm. Relative paths in configuration resolve from the declaring file.

```sh
actionlint check --plugin 'https://example.com/plugins/ruff.wasm.gz#sha256=<64-hex-digest>'
actionlint plugin inspect 'https://example.com/plugins/ruff.wasm.gz' --json
```

actionlint caches pinned downloads under the user cache's `actionlint/artifacts` folder and verifies them on
every use. It fetches unpinned URLs on every invocation. Remove a corrupt cache entry to download it again.
Downloads have a 30-second deadline and a 128 MiB limit before and after decompression. actionlint refuses HTTPS to
HTTP redirects and URI userinfo.

To inspect a registry plugin, use its alias or package name, with an optional
version:

```sh
actionlint plugin inspect shellcheck
actionlint plugin inspect github.com/kjanat/actionlint-plugin-shellcheck@v0.3.1 --json
```

Without a version, inspection selects the latest release. It verifies the plugin and shows its metadata without running
it or changing your configuration.

The `actionlint.plugin` custom section holds the plugin's JSON metadata. Only the outermost module or component counts.
actionlint rejects duplicate sections, duplicate JSON keys, invalid UTF-8 and metadata over 1 MiB. A core script
engine's `metadata()` result must match the section. A component's `describe` supplies its rule name, description and
settings schema.

## Contribute a rule

Choose the contract that fits the integration:

| Contribution                             | Public contract                                         | Authoring path                                                                          |
| ---------------------------------------- | ------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| Workflow analyzer or runner provider     | Versioned WIT interfaces                                | [Component examples](#write-a-plugin)                                                   |
| Embedded script engine                   | Versioned WIT or core Wasm JSON ABI                     | [Script engines](#script-engines)                                                       |
| External executable or container scanner | Version 1 process-provider manifest and diagnostic JSON | [TypeScript SDK](../plugins/sdk/typescript/README.md), including jq tests and packaging |

Publish through the [registry submission workflow](https://github.com/kjanat/actionlint-registry/blob/master/docs/publishing.md).
Run the candidate fixture against the exact artifact and intended host version.
Open an issue here to add a plugin to the documentation.

## Configure registered rules

Loading a document analyzer enables it.

Presets, `lint.enabled`, `lint.rules.disable` and
per-file overrides apply to plugin rules. The `recommended` preset follows the descriptor. `all` skips rules with
`maturity: nursery` unless you pass `--experimental`. actionlint validates options against the descriptor's bundled JSON
schema and refuses external schema references.

Core script engines with a bundled adapter use that adapter's settings, such as `tools.ruff`.
