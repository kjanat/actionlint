# Plugins

Plugins can be distributed as WebAssembly components, supported core Wasm script engines, or
[process-provider manifests](#local-tool-provider-manifests) for external tools.
The [TypeScript SDK](../plugins/sdk/typescript/README.md) supports manifest authoring,
jq tests and packaging. See [contribution routes](#contribute-a-rule) to choose an interface.

Commitish is the reference resolver bundled with actionlint. Resolver plugins
export `kjanat:actionlint-resolver/reference-resolver@0.1.0` and request exact
repository references and objects through the `repository-objects` import.
Actionlint performs those requests and records their network usage. The
`repository.id` field of `repository-objects` and the `repository-id` field of
the resolution are `option<u64>`: a resolver returns `none` when the repository
ID is unknown, and the request's `expected-repository-id` is `none` when
actionlint has no ID to expect. Loading a
resolver plugin replaces Commitish for that analysis. See
[reference resolution](remote-actions.md#reference-resolution).

actionlint runs WebAssembly components that implement the `kjanat:actionlint/analyzer@0.2.0` interface, defined in
[`plugins/protocol/wit/analyzer.wit`](../plugins/protocol/wit/analyzer.wit). A plugin first describes the checks it provides and the data it needs.
For each analysis call, it receives one document and the requested data, then returns diagnostics. Components may instead implement the independent
`kjanat:actionlint/runner-provider@0.2.0` interface or the
`kjanat:actionlint/script-engine` interface, version
[0.2.0](../plugins/protocol/wit/script-engine.wit) or
[0.3.0](../plugins/protocol/wit/deps/actionlint-0.3.0/script-engine.wit). Components can implement multiple
roles. Any language with a component toolchain can implement them.

Tool integrations are discovered through the optional
[`tool-integration`](../plugins/protocol/wit/tool-integration.wit) export,
regardless of repository name, registry alias or loading path. A conforming fork receives
the same configuration, suppression and execution behavior. Before execution,
the plugin receives configuration produced by the provider's manifest and caller-supplied
plugin options as separate arguments. It accepts or rejects the resulting
configuration and declares the resources it needs. Falling back to a native tool
requires the caller to have enabled native execution.

The [ShellCheck integration](https://github.com/kjanat/actionlint-plugin-shellcheck/blob/master/docs/configuration.md)
uses `source-files` to map host paths to guest filesystem paths and
[`shell-script-inputs`](../plugins/protocol/wit/deps/actionlint-script/shell-script-inputs.wit) to obtain prepared shell text. The host supplies interpreter and environment facts, masks expressions and maps
generated lines to source locations. The integration interprets ShellCheck directives
and selects the engine options and excluded diagnostic codes.
Filesystem access stays within the workspace selected by the host. Configured
source paths and symlink targets that resolve outside it fail analysis; missing paths receive no access.
Plugin options do not carry filesystem mount metadata. The
[`template-effects`](../plugins/protocol/wit/template-effects.wit) interface
identifies which diagnostics depend on masked workflow expressions, so the host
can filter them. The host never infers those effects from diagnostic wording or
rule codes. Compatibility depends on the WIT types. Source-file hashes record
which interface files were used to build the plugin.

The alternative [core JSON memory ABI and versioned operations](../plugins/protocol/wit/core-json-abi.md)
are specified alongside WIT. Check operation 2 carries the same template-effect semantics.

## Install from the registry

Install ShellCheck, Ruff or Shuck by name:

```sh
actionlint plugin install shellcheck
actionlint check .github
```

Shuck includes its scanner in WebAssembly. Enable its nursery rule with
`--experimental` or `lint.rules.correctness.shuck`.

The `zizmor`, `poutine`, `runner-guard`, `syft`, `grype`, `grant`, `yamllint`,
`psscriptanalyzer` and `trailing-space` examples are not published in the registry.
The external-linter examples need Node and their scanner on `PATH`.
PSScriptAnalyzer also needs PowerShell and the PSScriptAnalyzer module.

### Install a preview or CDN artifact

Pass the manifest URL directly to actionlint:

```sh
actionlint plugin install 'https://esm.sh/pr/kjanat/actionlint/@kjanat-actionlint/plugin-yamllint@afaa8f8/actionlint-provider.json?raw'
actionlint check --allow-config-processes .github
```

actionlint downloads the file, validates it, saves its SHA-256 in the config and
caches it for later checks. No npm installation or `node_modules` path is needed.
The same command accepts HTTPS plugin files from jsDelivr and other servers.

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

The repository names above are placeholders; `search` shows what is actually published. The catalogue may be empty.
Use `--registry https://example.org/catalogue/` on search, versions or install to select another compatible registry.
Registry metadata requests stay within that HTTPS origin and catalogue directory. Artifact URLs must match the
registered GitHub release or GitLab generic package.

Installation updates the root `plugins` list in your existing `.github/actionlint.yaml` (also `.yml`, `.json`
or `.toml`), creating `.github/actionlint.yaml` when no config exists:

```yaml
plugins:
  - github.com/owner/plugin@v1.2.3
  - gitlab.com/group/plugin@v1.2.3
```

The registry explicitly assigns short names such as `shellcheck` and `ruff` to published plugins.
Names are never inferred from a repository prefix or claimed through plugin metadata.
Install resolves an alias through its selected registry and writes the canonical package
and exact version into configuration. An alias reassignment cannot change that declaration.
Plugins without an assigned alias use `owner/plugin`, `github.com/owner/plugin` or `gitlab.com/group/plugin`.
A new dependency without `@version` selects the newest stable version. If the
plugin has only prereleases, it selects the newest prerelease. Installation
writes the exact selected version into configuration.
Reinstalling an existing dependency preserves its declared version and registry unless explicitly overridden.
Use an object for a custom registry:

```yaml
plugins:
  - name: github.com/owner/plugin
    version: v1.2.3
    registry: https://example.org/catalogue/
```

When a TOML config has no plugin declaration yet, installation appends plugin tables
at the end, after its existing tables. Existing declarations are updated in place:

```toml
#:schema https://cdn.jsdelivr.net/gh/kjanat/actionlint@2/actionlint.schema.json

[lint.rules]
preset = "recommended"

[[plugins]]
name    = "github.com/kjanat/actionlint-plugin-shellcheck"
version = "v0.2.0"
```

A direct artifact in table form uses only `source`, containing the digest-pinned URL
or OCI reference. It cannot also declare `name`, `version` or `registry`.

Commit the configuration with each plugin's canonical package and exact version.
The selected registry supplies publisher identity, artifact location,
format, contract and checksum from its immutable release record. Verified resolutions are cached
under the user's `actionlint/registry` cache, separately for each registry, package and version.
Downloaded bytes are checked against that checksum, including compression. A changed record for
an already observed release is refused. This trusts the configured registry to maintain its
immutable release mapping. Registry validation does not guarantee plugin safety or correctness.
Install verifies the download and embedded metadata without executing guest code. Actual ABI/capability checks
run when the plugin loads. Registry validation and reports for another host revision do not guarantee compatibility.
`versions --json` includes the registry's host-specific compatibility evidence; missing evidence is not a failure.

For new dependencies, omitting `--version`, or using `--version latest`, selects the highest stable full semantic version. Prereleases
and non-semantic tags require exact selection. Equal-precedence tags are ambiguous and require an exact tag too.
Updates do not advance exact tag selectors. To change an installed selector:

```sh
actionlint plugin update github.com/owner/plugin --version latest
actionlint plugin update
actionlint plugin sync
actionlint plugin remove github.com/owner/plugin
```

Declarations in `plugins` must name exact canonical releases. Run `actionlint plugin install`
to resolve an alias or `latest` and save the selected version.
An uncached exact declaration obtains its registry record once. `sync` restores the configured artifacts into
the shared user cache and refreshes registry withdrawal information, useful after cloning or in CI.
Checks also restore missing pinned artifacts; populated
caches work offline and cached bytes are re-hashed. No implicit upgrades run during linting or LSP analysis.
`latest` refuses downgrades from a stale catalogue; an explicit exact tag permits a deliberate downgrade.
Failed downloads, immutable-release conflicts and concurrent config edits leave configuration unchanged.
Updates acquire every selected artifact before saving config. Removal edits the config and leaves shared
cache entries intact. Checking, synchronization, rule discovery and LSP analysis never rewrite project files.

A new release may change its artifact filename or migrate from a core module to a component.
An explicit update records that release's format, filename, interface and digest. Updates reject changes to the
publisher, repository or plugin identity, and reject altered bytes, format or filenames for an existing release.
Checking, synchronization and installation with an already matching pin use the pinned artifact.

The discovery metadata's `id` identifies the package in the registry.
Its analyzer descriptor names the rule; that name can differ from the package ID.
The tool-integration identifier must equal the analyzer rule name.

Edit `plugins` directly, then run `actionlint plugin install` without arguments to resolve missing or changed
dependencies. Existing matching pins stay pinned. Checks use declared, verified exact releases.
`plugins: []` or `plugins: null` disables all declared plugins,
including inherited ones. An inherited list is replaced as a whole; install/remove edit only the selected leaf config.

`actionlint config init` installs and pins `shellcheck` and `ruff`. Migrating an old configuration
adds these defaults when `plugins` was omitted, respecting disabled checks and explicit native backends.
All downloads must verify before the migrated config is saved.
See [configuration migration](config-migration.md) for preview and explicit migration behavior.

Checks also use these registry plugins when no plugin list was configured. The
user cache records the selected releases. `actionlint cache warm` refreshes them.
An explicit `--tool` command or native backend selects the native linter.

### Try an unpublished candidate

Use a local candidate catalogue to install a packaged plugin before creating a release or registry submission:

```sh
actionlint plugin install owner/plugin@v1.2.3 --registry file:///absolute/path/to/candidate/
actionlint check
```

On Windows, use `file:///C:/path/to/candidate/`. The registry's candidate exporter creates this directory
from a packaged artifact. Installation writes configuration, verifies the artifact's
digest and metadata, and binds the declaration to the verified SHA256 digest. The digest is part of the
configuration entry and does not make a claim about publisher authentication or publication. Candidate
catalogues are accepted only through an explicit local file URI. Their records and artifacts must stay within
that directory.

Checking uses the selected digest's imported bytes even after the candidate directory is removed. Pins are kept
in a per-digest user cache, so rebuilding a candidate at the same version does not change installed projects.
Use `plugin update owner/plugin` to select rebuilt bytes; that updates the digest in configuration. A cold cache
can restore only when the local catalogue still contains the selected bytes. Published versions still reject
replacement. Run candidate checks in a temporary project to keep local paths out of a production configuration.

### Deprecated, withdrawn and revoked releases

Deprecated versions can be installed with a warning. Withdrawn versions are excluded from new resolution; an existing lock can continue
using them with a warning. Revoked artifact digests are refused, including already cached bytes.

Install/update obtain current status when resolving versions. `plugin sync` refreshes each registry's
status snapshot; if unavailable, it warns and uses previously recorded information. Ordinary checking
uses the local snapshot without a registry request. An offline machine cannot learn a new revocation
until a refresh succeeds. Once learned, a digest revocation cannot be erased by a later incomplete
snapshot. Direct URLs are outside registry withdrawal policy.
The language server checks this local status before loading plugins and before
each analysis. A revocation learned by another process stops subsequent analyses
using that plugin without requiring a server restart or a network request.

### Pin a directly hosted artifact

An HTTPS URL can appear in the same list, with a mandatory SHA256 pin:

```yaml
plugins:
  - "https://example.org/plugins/check.wasm#sha256=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
```

Replace the example digest with the downloaded file's SHA256. `actionlint plugin inspect URL`
reports the digest without executing the plugin. `actionlint plugin install "URL#sha256=…"`
verifies and caches the artifact before saving the declaration.

Direct declarations contain their exact artifact pin. Changing the artifact requires changing its URL
or digest explicitly. Registry withdrawal information does not cover independent direct URLs.
Remote execution through `--plugin` or configured Wasm backends also requires a digest pin.

### Acquisition and redirects

Repository configuration and inherited configuration may select only
public network destinations. This applies to the initial request and same-authority
redirects for artifacts and registry records. A digest verifies bytes; it does not
authorize private network access.

An explicit `--plugin` source, direct inspect/install URL, public plugin API source,
or `--registry` endpoint authorizes its selected scheme, host and effective port for
that invocation, including a private endpoint. Saving the declaration does not save
that authority. Selecting a configuration, running install/sync, and
`--allow-config-processes` do not grant private network access. Configured script
engines use the same public-only acquisition policy.

A redirect to a different authority must resolve entirely to public addresses.
Independent public artifact hosts and CDN redirects remain supported; there is no fixed hostname allowlist.
IPv4-mapped IPv6 addresses are checked as IPv4. Special-purpose, transition and
documentation ranges are refused, conservatively including protocol-assignment
ranges with public exceptions and IPv6 outside global `2000::/3`.

For every request without a matching invocation grant, the downloader resolves once and connects only to numeric
addresses from that checked answer. It retains the original HTTP Host, TLS
server name and redirect base. A later DNS answer cannot redirect that connection
into a private network. Existing timeout, size, digest and HTTPS-downgrade checks
still apply; a digest alone does not authorize contacting another endpoint.

Proxy selection uses the original URL, preserving environment proxy and
`NO_PROXY` behaviour. Direct connections and HTTPS targets through an HTTP
proxy are supported; CONNECT names the checked numeric destination. For requests
requiring public address screening, plain HTTP through a forward proxy, HTTPS/SOCKS proxies, custom TLS
dialers and nonstandard global transports are refused explicitly when destination
pinning cannot be established. They are not silently bypassed or disabled.
Requests covered by an explicit endpoint grant retain their existing transport behaviour.
These host acquisition rules are independent of a plugin guest's network permissions.

### Pull from an OCI registry

A plugin or tool provider can also be published as an OCI artifact, for example to GHCR with the
`oras` CLI. Declare it by digest:

```yaml
plugins:
  - "oci://ghcr.io/owner/provider@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
```

The digest names an image index or an image manifest. An index selects the image manifest whose
platform matches this host's `GOOS/GOARCH`; a lone image manifest serves every platform. The selected
image manifest has exactly one primary layer:

- `application/wasm`: a WebAssembly module, loaded like any other plugin.
- `application/vnd.actionlint.tool-provider.v1+json`: a tool provider manifest with
  `transport: "process"`. It can embed program and jq source inline, as the TypeScript SDK does, or
  reference companion layers by digest. An `artifacts` list selects executable layers by platform;
  digest-backed transforms must also be layers of the selected image manifest.

Stage a built Wasm plugin in an OCI image layout with ORAS:

```sh
oras push --oci-layout ./plugin-layout:example --artifact-type application/wasm plugin.wasm:application/wasm
oras manifest fetch --oci-layout ./plugin-layout:example
```

This creates a local packaging result. Publishing it to an OCI registry and registering it in the
actionlint catalogue are separate operations. The registry accepts release assets and digest-pinned
OCI sources, including process providers. Its shared candidate check accepts `--oci-layout` to test
the exact local graph before uploading it; see the
[OCI publication checks](https://github.com/kjanat/actionlint-registry/blob/master/docs/publishing.md#oci-checks-before-upload).

Every manifest and layer is verified against its digest before it is stored in the artifact cache,
`actionlint/artifacts/<sha256 hex>`. A blob that does not match its digest is never stored; a stored blob
that no longer matches, such as one left by an interrupted download, is fetched again. A provider's
executable is copied to `actionlint/artifacts/exec/<sha256 hex>/<filename>` with mode `0755` (`.exe` is
added on Windows) and re-hashed on every use.

Tags are not pins. `actionlint plugin install oci://ghcr.io/owner/provider:v1` resolves the tag once and
declares the digest it names; everywhere else, including `--plugin` and `plugin inspect`, a tag is refused.
The registry is reached anonymously, answering its bearer challenge as public GHCR images require.
Registry requests, token realms, retries and blob redirects follow the acquisition policy above.
Explicitly selected `localhost` and loopback registries use plain HTTP; repository configuration
cannot authorize them. Each pull has the 30-second deadline.

The configured OCI digest binds the index, selected platform manifest and layers. No project metadata
duplicates that graph. Cache planning reads the index and this host's manifest to enumerate the required
digests without downloading executable layers. Install and sync populate this host's artifact cache;
analysis verifies the manifests and bytes it uses.

A tool provider from an OCI source runs a native executable, which no sandbox contains. One declared in
repository configuration runs only with `--allow-config-processes`; the GitHub Action never sets it. A source
passed with `--plugin` is an explicit choice and needs no opt-in.

Analysis first reads only the provider manifest layer. The executable and transform layers are downloaded
after the `--allow-config-processes` check passes, so a refused provider never downloads its executable.
`--no-tool ID` skips a provider whose `rule.name` is `ID` the same way: its manifest layer is read to learn
the name, its executable is not. `plugin install` and `plugin sync` download every layer for this host, so
analysis can run offline afterwards.

### GitHub Action caching

The JavaScript Action loads the same declarations and exact registry releases as the CLI.
Its `plugin-cache` input defaults to `true`. Before downloading or starting plugins, it restores
only the selected digest-named artifact files from GitHub Actions cache. The key contains the
operating system and exact set of digests. Every restored file is rehashed before use.

Verified artifacts are saved even when linting reports findings. Cache service failures produce
warnings and leave ordinary downloads available. Set `plugin-cache: false` to disable this
optimization; compiled runtime state is never stored in this cache.

The Action never installs native Ruff or ShellCheck executables or adds them to PATH.
Any component declaring the ShellCheck integration profile uses `tools.shellcheck` and
the ordinary `shellcheck` rule controls. The component negotiates rc-file support and source
grants. Direct URLs, `--plugin`, and registry declarations select identical integration behavior.
Opaque plugin options use `lint.rules.plugins.shellcheck.options`; explicit library registration
options take precedence. Components without a tool-integration export run as standalone analyzers.

Check, fix, rules and LSP load the exact artifacts declared for the selected plugins. The following rules determine which plugins are selected:

- Explicit `--plugin` sources replace automatic selection.
- `--no-config` (or `ACTIONLINT_NO_CONFIG`) disables automatic plugin selection; explicit sources still work.
- Explicit `--config FILE` selects that config's declarations, with no repository fallback.
- Otherwise declarations come from the nearest repository's `.github/actionlint.*`.
- `--no-tool ID` (or the Action's `disable-tools` input) drops plugins that provide the tool `ID`: a selected plugin
  whose name or alias is `ID` is not downloaded, and any other plugin whose static metadata names `ID` as its engine
  or one of its rules is not loaded.

Checks spanning repositories with plugin declarations require separate invocations or an explicit invocation-wide selection.
LSP snapshots the startup selection; restart after installing, updating or removing plugins. Automatic project
plugins cannot apply to documents from another repository. Use a separate server or an explicit config for that scope.
Select a runner provider's execution target with `--runner-target`.
Management commands accept `--config FILE` too. Outside a Git repository, select a config with `--config FILE`.
Management commands reject `--no-config`.

## Load a plugin directly

```sh
actionlint --plugin ./pipe-to-shell.wasm --plugin ./no-sudo.wasm
```

Each `--plugin` loads one component or a supported core script engine. Analyzer findings carry the plugin name as their rule, so `--ignore-regex`, inline
suppressions and configured ignores apply to them like built-in rules. `--log-level debug` prints the plugin's limits
when it loads and forwards its log lines, stdout and stderr.

### Suppression ownership

Actionlint interprets its own YAML directives (`actionlint:ignore`,
`actionlint-ignore`, `ignore[...]` and the next-line forms) and configured
`ignores`. Plugins return findings that their own directives have not suppressed.
Actionlint validates their source ranges, maps snippet positions to YAML, then
applies its ignores and the repository's suppression policy.
Plugins must not reinterpret actionlint directives as native linter directives.
Selectors use the rule ID registered with actionlint; upstream check IDs are diagnostic
codes. The plugin handles native per-code suppression. Rule IDs must not
contain whitespace, commas or square brackets, so every actionlint selector can select them.

The plugin or wrapped linter interprets native directives in submitted code, such
as `noqa` and `shellcheck disable`. Actionlint does not translate them. The
source-annotations interface supplies raw YAML comments without interpreting them.
Script providers receive prepared source; document providers receive captured YAML.
Actionlint can mask expressions, insert startup or configuration lines, and pass
attached YAML comments to a script provider. It tracks which lines came from the script, YAML comments, or generated text;
the plugin or linter interprets any directives.

`forbidden-suppression` governs actionlint's own inline and configured ignores.
It does not disable native linter directives or tool-specific filtering options,
and cannot recover findings a plugin did not return. A plugin may offer a native
suppression as a suggested source fix; actionlint validates that edit normally.
The built-in LSP suppression action always adds an actionlint directive.

The Go and SDK SARIF readers normalize native suppression metadata identically:
an `inSource` or `external` entry with no review status (including null) or an
`accepted` status suppresses the result. `underReview` and `rejected` entries do
not; one applied/accepted entry is sufficient. Invalid kinds/statuses fail decoding.
These SARIF suppressions apply to the tool's results. Actionlint evaluates its own
diagnostics and suppression settings separately.

Pass the same `--plugin` arguments to `config show`, `config validate` or `doctor` when the configuration names plugin
rules. These commands validate rule IDs against loaded descriptors without running document analysis. Pass them to
`config schema` to print a configuration schema that describes the tool providers they install.

The language server closes loaded plugins at shutdown. Explicit `--plugin` selections and remote engine URLs
use a fixed snapshot for that session; restart to refresh them. Configured local engine files reload when their
content changes, and selecting a different engine URI loads that identity separately. Active analyses can finish
using the engine they started with.

The host refuses to load a component when it exports an unsupported version of a recognized interface, imports an interface
the host does not grant, or exports no supported role. Before guest startup it checks each implemented interface's complete
type graph against the canonical WIT, including function signatures, record fields and enum ordering. The expected
graph and enum discriminants are generated with `go generate ./internal/plugins/execution/component`; a test detects stale output.

## Limits and capabilities

Every analysis call runs in a fresh instance with a shared 64 MiB limit across live linear memory and reserved core
tables in its plugin runtime. Its 10-second timeout starts when the call leaves
the queue and covers instance setup and guest execution. Queue waiting honors the caller's cancellation and
deadline; it has no separate timeout. A trap, a panic, an exit, an
exhausted limit or a cancelled run fails the analysis of that document with an error that names the plugin and its
limits. The run then continues with the next document and actionlint exits with status 3.

Component results and host-import arguments also have per-call budgets of 262,144 values and 8 MiB of accounted
string and collection storage. Nested values and repeated aliases count toward these budgets; validation runs
before host allocations and string encoding conversions.
The maintained wacogo fork adds these allocation checks. See the
[Go dependency graph](go-forks.md) for its selected version, upstream origin and
recursive dependencies.

Stdout, stderr and the direct `host.log` capability share a 1 MiB output budget per call, including when debug logging is disabled. Exceeding it fails
the analysis even if the plugin handles the write error. Log lines are emitted in chunks of at most 64 KiB, and an
unfinished line is flushed at the end of its call.

The host grants these imports and nothing else:

| Interface                        | What the plugin gets                                                                        |
| -------------------------------- | ------------------------------------------------------------------------------------------- |
| `kjanat:actionlint/host@0.2.0`   | `log`, one line into actionlint's debug log                                                 |
| `wasi:cli/*`, `wasi:io/*`        | stdout and stderr into the debug log, empty stdin and environment                           |
| `wasi:clocks/*`, `wasi:random/*` | clocks and random numbers                                                                   |
| `wasi:filesystem/*`              | no directories by default; explicitly granted read-only snapshots for source-aware adapters |

`wasi:sockets`, `wasi:http` and custom interfaces are refused at load.

Go embedders can supply `analyzer.Input.Files` for one `Analyze` call. Each entry
maps a caller-owned read-only filesystem to an absolute guest path. The caller
keeps it open until analysis returns and supplies directory-relative read
operations. A later call inherits no grants. Ordinary standalone analyzers receive
no filesystem access automatically. The managed ShellCheck adapter grants only
the workspace and explicitly configured source directories, records source
dependencies and translates guest diagnostic paths back to their host files.

Load a component with `--plugin` or a configured plugin declaration. Plugin authors
use the [guest SDKs](api.md#write-and-test-a-plugin); host runtime packages are private.

## Runner providers

[`plugins/protocol/wit/runner.wit`](../plugins/protocol/wit/runner.wit) defines the separate `runner-plugin` world. A provider-only
component exports `describe` and `resolve`.
The host resolves a batch of complete `runs-on` selectors before running checks.
All labels within a selector must match the same runner. The request preserves
runner groups, source positions and unresolved expressions. It keeps each
matrix combination separate, so labels from different combinations are not
mixed. An incomplete set of matrix alternatives cannot establish the
execution environment.

Runner validation, shell validation, script analysis, local action resolution
and executable checks use the same runner facts. The built-in GitHub catalogue
supplies facts through the same interface. Installing a provider does not select
its target: `--runner-target` defaults to `github`, so choose another target
explicitly. If several providers apply, select one with `--runner-provider`.
Providers do not change the runner labels in configuration.

Results distinguish resolved candidates, no match, unknown and not applicable. An operational error fails analysis;
independent checks still run, while runner-dependent inference stays unknown. No match produces a `runner-label`
finding only with a fresh, complete dataset. A stale, missing or incomplete dataset cannot prove an invalid label or a default shell.
Explicit shells and container defaults take precedence over provider defaults.

Configured self-hosted labels form an allowlist. A literal label matching that list with `windows` as a complete,
case-insensitive hyphen-separated segment supplies Windows and the `pwsh` default, including vendor-prefixed names.
This convention does not accept an unconfigured label or infer an OS from arbitrary substrings. Contradictory OS
labels produce a runner conflict and leave execution facts unknown.

For custom environments that do not follow that convention, select a runner provider and supply its snapshot.
The provider returns each candidate's explicit `os` and `default-shell`; these shared facts drive shell validation,
script analysis and path interpretation. The snapshot format belongs to that provider, as the queue example below
illustrates. The built-in GitHub provider does not interpret another provider's snapshot. A runner's OS does not
select a script linter: script engines declare the shells they analyze. Missing or stale runner data leaves
the default shell unknown; an explicit workflow shell still takes precedence.

### Custom GitHub runner inventory

Use the built-in `snapshot` provider for arbitrary labels such as `desktop-pool`, without writing a plugin:

```sh
actionlint --runner-provider snapshot --runner-scope my-org/my-repo \
  --runner-snapshot runners.json workflow.yml
```

`runners.json` uses the same snapshot envelope as external providers. Its `content` contains the runner inventory:

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

Each entry requires a unique, nonempty `name`. `labels` lists the labels assigned to that runner; labels match
case-insensitively. An optional `group` matches the requested group exactly. All labels and the group must match
one entry. Include only runners eligible for the supplied repository scope.
Matrix alternatives resolve separately; ambiguous candidates contribute only facts on which they agree.

Optional facts are `os` (`linux`, `macos`, `windows`, `unknown`), `architecture`, `default_shell`,
`case_sensitivity` (`sensitive`, `insensitive`, `unknown`), `sh_is_dash` and `ephemeral`.
Declare each fact explicitly; Windows alone does not establish that PowerShell is installed.
Unknown JSON fields and invalid enum values are errors. An empty `runners` array asserts no eligible runners
only when the snapshot is fresh and complete. Missing, stale or incomplete runner data cannot establish a default shell
or reject a selector. Freshness and completeness are assertions supplied by whoever maintains the inventory.

Selection is explicit: supplying a snapshot alone does not replace the default `github` provider. The `snapshot`
provider uses only the inventory, without adding hosted labels or configuration allowlists. Include every eligible
candidate relevant to the workflows being checked before marking the inventory complete. These flags also work with
`actionlint lsp`; restart it after updating the snapshot.

For this inventory, `runs-on: desktop-pool` supplies Windows and `pwsh` to runner checks, shell inference and script
routing. Load a script-engine plugin advertising `capabilities.script_shells: ["pwsh", "powershell"]` to analyze
PowerShell snippets. The host selects engines by the effective shell; no `script-linter` setting is needed on a runner.
This does not install a PowerShell analyzer. Without a matching engine, those snippets receive no engine diagnostics.
ShellCheck handles an explicit `shell: bash` override. Step, job and workflow
shell declarations override the inventory; a job container supplies its own `sh` default.

### Offline Buildkite example

[`plugins/examples/buildkite-runners/src/lib.rs`](../plugins/examples/buildkite-runners/src/lib.rs) resolves an explicit queue mapping
snapshot. This offline example uses supplied queue data without live Agent API calls or guarantees of GitHub image/toolchain parity.
Its [README](../plugins/examples/buildkite-runners/README.md) includes tests, SDK packaging and a command using the supplied workflow and snapshot.
Each queue declares its name, labels, optional group, OS, architecture and default shell. All labels and the requested
group must match the same queue. The catalogue's completeness refers to the selected scope.

```sh
cargo build --locked --profile wasm --target wasm32-wasip2 -p actionlint-example-buildkite-runners
actionlint --plugin target/wasm32-wasip2/wasm/buildkite_runners.wasm \
  --runner-target buildkite --runner-provider buildkite-runners \
  --runner-scope my-org/my-repo --runner-snapshot runners.json workflow.yml
```

`runners.json` is a JSON envelope with provider-specific catalogue data in `content`.
The CLI passes that JSON to the provider as opaque content. Providers using a text format can instead supply a string.

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

The host reads this size-limited JSON object once per session. To refresh it,
replace the snapshot before the next CLI run, or restart the language server.
The snapshot explicitly states whether its data is fresh; analysis does not
expire it automatically or refresh it from the network. The snapshot and each
answer must use the requested scope. A provider cannot report fresher or more
complete data than the supplied snapshot allows.

The runner-provider WIT contract supplies the request and response types. The host
records the implementation ID, target, scope, snapshot digest and answers when
preparing runner knowledge. Its cache key includes those values and the
configuration, so a changed catalogue or artifact invalidates cached answers.
The host chooses which provider to trust. The Wasm sandbox restricts execution
but does not verify the provider's answers.

### Cross-runtime verification

[`internal/development/tests/conformance/wasm/runner-host`](../internal/development/tests/conformance/wasm/runner-host) compares the Go host and Wasmtime for a fixed set of runner-provider requests.
It uses the unchanged WIT and the exact same component bytes as the Go host. Build and compare normalized answers,
source positions, candidate guarantees, dataset status and artifact digest:

```sh
cargo build --locked --manifest-path internal/development/tests/conformance/wasm/runner-host/Cargo.toml --target-dir target/runner-host
ACTIONLINT_RUNNER_HOST="$PWD/target/runner-host/debug/actionlint-runner-host" \
  go test ./internal/development/tests/integration/plugins -run TestRunnerProviderWasmtimeParity -count=1
```

On Windows, set `ACTIONLINT_RUNNER_HOST` to the absolute `.exe` path before running the test.

## Write a plugin

The world is `plugin`: it imports `host` and exports `analyzer`. `describe` returns the plugin name and the facts it
needs. `analyze` receives the document path and content, declared scripts, scripts invoked through composite actions,
mapping names and the JSON options the host registered, and returns an `outcome` containing diagnostics and an optional
error message. An incomplete outcome can include diagnostics; a trap cannot return findings.

Script text comes with spans that map each line and column back to the declaring document. The WIT comment on `span`
gives the lookup. A diagnostic `rule` must be empty or equal the plugin name; use `code` for check identifiers.
Invalid rule names cause incomplete analysis; the result includes valid findings.
An empty `rule` takes the plugin name, and an empty `path` means the analyzed document, so findings in a composite
action copy `script.path`.

Plugins can opt into the [`source-annotations`](../plugins/protocol/wit/source-annotations.wit) interface by selecting the
`annotated-plugin` world. The existing `analyzer@0.2.0` interface and its contract digest are unchanged. The optional
host function `comments` accepts a complete script source reference and returns physical YAML comments attached
immediately before its declaration or after its scalar/header, with placement and Unicode source positions.
`script.text` includes comments inside the scalar. The source-annotations interface supplies attached YAML comments separately.
Queries are limited to the scripts supplied in the current analysis, including invoked composite actions. Unknown
references and calls outside analysis return no comments. Alias-use comments belong to that declaration; fixes still
target the anchored scalar containing the script.

Each plugin interprets its own comment syntax. The ShellCheck plugin treats these YAML placements as script-wide
ShellCheck directives:

```yaml
- name: Leading YAML directive
  # shellcheck disable=SC2086
  run: echo $HI
- run: echo $HI # shellcheck disable=SC2086
- run: | # shellcheck disable=SC2086
    echo $HI
```

Native directives inside `run: |` keep ShellCheck's normal scope. A trailing directive inside a quoted scalar or
block body is shell text, so ShellCheck reports invalid directive placement. Errors in attached directives point to the YAML
comment; script diagnostics and fixes keep their original scalar spans and document revisions.

### Rust

[`plugins/examples/pipe-to-shell/src/lib.rs`](../plugins/examples/pipe-to-shell/src/lib.rs) flags `curl ... | sh` patterns. It depends on
[`actionlint-extension-api`](../plugins/sdk/rust), which provides the generated guest types,
`Guest` trait, host imports and `export_plugin!` macro. Its [README](../plugins/sdk/rust/README.md)
contains a minimal analyzer implementation. The example builds with:

```sh
cargo build --locked --profile wasm --target wasm32-wasip2 -p actionlint-example-pipe-to-shell
```

The component is `target/wasm32-wasip2/wasm/pipe_to_shell.wasm` at the repository root.

Enable the SDK's `source-annotations` feature to import attached YAML comments.
The [source-annotations example](../plugins/examples/source-annotations/README.md) includes native tests,
component packaging and a workflow command. Without that feature, the SDK does not import comments.
The SDK's native `package` feature provides `package::Package`:
pass the plugin's descriptor, Cargo identity, engine, capabilities, languages and license text, then call `write`
with the component path and output directory. The SDK writes discovery metadata, schema and license sidecars,
and checksums. Plugins do not implement their own Wasm section traversal or vendor the SDK's WIT contracts.

### Shared declaration facts

[`plugins/examples/pinned-actions/src/lib.rs`](../plugins/examples/pinned-actions/src/lib.rs) and its
[native comparison check](../internal/development/tests/conformance/wasm/dependency-pins/check.go) request only `dependencies`.
Both flag remote action and reusable-workflow references that do not use a full commit hash, while excluding
local references and Docker images. They use the host's parsed `uses` values, dependency kinds, source identities
and scalar ranges; neither reparses the workflow or reads `input.content`.

```sh
cargo build --locked --profile wasm --target wasm32-wasip2 -p actionlint-example-pinned-actions
```

The component is `target/wasm32-wasip2/wasm/pinned_actions.wasm`.
`TestDependencyAnalyzerNativeComponentParity` checks identical findings from both implementations with empty,
invalid and unrelated source content. `TestDependencyAnalyzerSharedDeclarationFacts` checks the complete path
from workflow parsing to diagnostics, including quoted scalar ranges and revision-bound node references.
The example validates declaration facts already present in the contract; other checks may require additional facts.

### C

[`plugins/examples/no-sudo`](../plugins/examples/no-sudo) flags `sudo` at the start of a command. `build.sh` generates bindings with `wit-bindgen c` and compiles with wasi-sdk:

```sh
WASI_SDK_PATH=/opt/wasi-sdk ./build.sh no-sudo.wasm
```

### TinyGo

[`plugins/examples/insecure-download`](../plugins/examples/insecure-download) flags `curl -k` and
`wget --no-check-certificate`. It uses the Go SDK's guest bindings. Its build script combines the plugin contract
with TinyGo's WASI definitions, builds the component and adds discovery metadata from the guest's own descriptor.
From the repository root, run:

```sh
mise exec -- bash plugins/examples/insecure-download/build.sh
```

The [example README](../plugins/examples/insecure-download/README.md) shows its tests, release packaging and use
with actionlint.

### Other toolchains

Any toolchain that emits a component for the `plugin` world works.

### Build the examples

Run `mise run plugin-examples` from the repository root to build the Rust, C and TinyGo examples.
The Rust components are Cargo workspace packages. The C and TinyGo builds use their language toolchains.
Tests load the components through the plugin interface.

For an independent Rust analyzer, create a package with its own manifest and lockfile and depend on the SDK.
Other languages can vendor the public [`plugins/protocol/wit`](../plugins/protocol/wit) contracts and point their binding generator
at that copy. The host's fixture workspace is not a dependency of external plugins.

## Test a plugin

Run it against a workflow:

```sh
actionlint check --no-config --plugin ./plugin.wasm .github/workflows/ci.yml
```

The host loads the component and registers its declared rules before validating
configuration and suppression selectors. The internal integration tests also cover
composite action calls. The Rust crate under
`internal/plugins/execution/component/testdata/sources/misbehave` shows how traps, panics, exits, allocation beyond the limit, hangs and denied
file access are reported.

## Script engines

For Python and shell snippets, actionlint selects the snippet,
masks workflow expressions and maps positions back, and the engine checks one sanitized snippet at a time.
The preferred component interface is [`script-engine.wit`](../plugins/protocol/wit/script-engine.wit), world
`script-engine-plugin`. It has typed `describe`, `validate-config` and `check` operations. Plugin-specific
configuration is an engine-validated JSON object; shared descriptors, diagnostics and outcomes use WIT
records. Coordinates count one-based Unicode scalar values, with exclusive range ends. An outcome can contain
both findings and an incomplete-analysis error.

The host accepts two versions of the interface. A component exports exactly one.
Before running guest code, the host checks every exported type against that
version's definition, including function signatures, records and enums. Other
versions are rejected.

- `kjanat:actionlint/script-engine@0.2.0` ([`script-engine.wit`](../plugins/protocol/wit/script-engine.wit))
  returns diagnostics without fix edits and reads no files. The optional
  [`template-effects@0.2.0`](../plugins/protocol/wit/template-effects.wit) export pairs with it.
- `kjanat:actionlint/script-engine@0.3.0` ([`0.3.0/script-engine.wit`](../plugins/protocol/wit/deps/actionlint-0.3.0/script-engine.wit))
  declares in its descriptor the capabilities that a process-provider manifest declares for a native tool.
  The descriptor's `capabilities` record declares `fixes`, `reported-sources` and the `resources` the
  engine needs for each check. The request adds `excluded`, diagnostic codes selected during source preparation
  (strings such as `SC1091`), and `directory`. Each diagnostic adds `fixes`, edits in snippet coordinates,
  and `source`, a file the engine read besides the snippet. The diagnostic severity is the engine's own string,
  such as `style`; the host maps it as it maps a process tool's severity. A template-aware engine pairs it with
  [`template-effects@0.3.0`](../plugins/protocol/wit/deps/actionlint-0.3.0/template-effects.wit), whose check
  takes the 0.3.0 request and wraps 0.3.0 diagnostics, so effects, fixes, reported sources and the directory
  grant combine. A finding in a reported source has the effect `none`. The host rejects a template-effects export
  of the other version.

The only resource kind is `directory`, which an engine can request to read the script's working
directory. When that directory is known, the host opens it through the workspace
root and mounts it read-only at `/directory` for that call. The request's
`directory` field contains that guest path. A working directory outside the
workspace, including a symlink that leads outside it, fails the check. A missing
directory receives no access. Without access, `directory` is none and the engine
sees no files.

To report a finding in a file it read, the engine must declare
`reported-sources` and have directory access for that check. The diagnostic's
`source` is a relative slash-separated path below the request directory, without
`..`, backslashes or colons. Its positions refer to that file, and it cannot
include fixes. The host reads the file through the same directory access before
displaying its contents. It rejects diagnostics
that use fixes or reported sources without declaring those capabilities.

The host records files read through the directory for watch mode. The engine
reads those files from disk, as a native tool does. The caller's read overlays
apply to workflow files; they do not replace files read by an engine.

[actionlint-plugin-ruff](https://github.com/kjanat/actionlint-plugin-ruff) supplies a component and a legacy core
module built from the same analysis implementation. Both use the existing Ruff expression sanitizer, configuration
and YAML mapper. Select either artifact through `--plugin`, the registry, or the tool configuration:

```yaml
tools:
  ruff:
    backend:
      wasm:
        path: ./tools/plugin.component.wasm.gz
```

Component loading checks the
typed interface, runtime descriptor and static declaration before registering the engine. Component engines use
the same component execution limits and lifecycle as the other component roles.

Core modules use the version 1 JSON ABI: `check`, `validate_config` and `metadata` exports, no imports,
and UTF-8 JSON in linear memory. Check version 3 carries the same additions as script-engine 0.3.0 and is preferred
when a module advertises it; modules that advertise only check 1 or 2 work unchanged (see
[`core-json-abi.md`](../plugins/protocol/wit/core-json-abi.md)). The host checks the ABI version, export signatures and advertised operation versions,
refuses modules with imports, and runs each call in a pooled instance under a 256 MiB memory limit and a 10 second time
limit. Defined tables require finite maxima; conservative reservations for their maximum size and growth buffers share
that memory budget with linear memory. Components charge each core instance, including repeated and nested modules;
imported table aliases are charged to their defining instance only. Artifact acquisition defaults to 128 MiB for both raw and gzip-expanded bytes. Every response, including metadata
and configuration validation, must fit within 8 MiB before the host reads, copies or decodes it.
Initialization and creation of pooled instances use the same time limit. Native compilation has a separate
one-minute deadline; a cold compilation does not consume the guest execution budget. An earlier caller deadline
still applies to both phases. Compiler cancellation is cooperative, and an expired compilation never proceeds
to guest initialization. A trap or an exceeded limit, including
during allocation cleanup, discards that instance and fails the document's check.

### Local tool provider manifests

Any native command that checks a snippet on stdin can run as a script tool.
Describe the command in a tool provider manifest and pass it with `--plugin`:

```sh
actionlint --plugin ./tools/mylint/manifest.json
```

The manifest (`schema_version: 1`, `transport: "process"`) names the rule (`rule.name` is the tool ID used by
`tools.<id>` and `lint.rules`), the scripts it receives (`selectors.shells`, `selectors.inputs`, `selectors.filename`)
and how to run the command:

- `process.entry` is a command found on `PATH`. A local manifest declares no `artifacts`.
- `process.args` holds literal arguments and a closed set of placeholders that are substituted, never
  evaluated: `{var:NAME}`, `{flag:NAME:--opt}` (`--opt VALUE`), `{option:NAME:--opt}` (`--opt=VALUE`),
  `{switch:NAME:--on:--off}`, `{list:NAME:SEP}`, `{config:toml-pairs:--opt}`, `{config:toml-file}`,
  `{config:json-file}` and `{user-args}`.
  `{var:filename}` is the snippet's virtual name.
- `process.stdin` is `source` (the masked snippet) or `request` (the JSON check request).
- The command runs with a scrubbed environment: only `process.env` and the variables named in
  `process.env_passthrough` reach it. List `PATH`, `HOME` or `TEMP` there if the command needs them.
- `process.exit_codes` lists accepted statuses (default `[0]`); a nonzero status needs nonempty stdout.
- `process.output.format` is `script-diagnostics@1` (the `{ok, diagnostics, errors}` envelope), `sarif@2.1.0`,
  or `json`/`text` reshaped by a jq transform. `process.output.coordinates` states the tool's row, column and end
  conventions when they differ from LF rows, character columns and exclusive ends.
- Transforms are named by `digest` (`sha256:<hex>` of a `.jq` file beside the manifest) or given `inline`.
  A transform sees stdout as `.` and the argument variables, plus `exit_code`, as `$vars`. It has no environment,
  modules or `input`, and returns a diagnostics array or `{diagnostics, sources, errors}`.
- `capabilities.message_template` formats messages from `{code}`, `{severity}`, `{row}`, `{column}` and
  `{message}`; `capabilities.fixes` and `capabilities.reported_sources` admit suggested edits and findings in files
  the tool read.

Inline `tools.<id>.config` settings reach the tool through `process.config.render`, after an optional
`process.config.transform`. The manifest replaces a bundled tool with the same rule name for that invocation.

An integration can also declare `configuration_transform` to normalize its settings before either a process
or a managed component receives them. This jq program receives `{config, config_file}` and returns an object
with `options`. It can also return `source_prefix` (generated comments ending in a newline), `language` and
`read_access: {enabled, directories}`. Shell preparation uses the language and source-access settings;
expression-mask preparation accepts options and generated comments. Python preparation accepts converted options.
The host checks the original settings
against `config_schema` before running the program. A process can validate the converted options with
`process.config_schema`.

List path settings in `configuration_paths` as JSON pointers, for example `["/source-path"]`. The host expands
configuration variables in those strings or arrays of strings before running the transform. Other strings,
such as regular expressions, are passed through unchanged. Requested directories must pass the host's
workspace and filesystem checks before a component can read them.

Use `preparation_transform` when tool options depend on the script or its interpreter. It receives:

- `source`, `language` and `startup`: the original script, inferred interpreter and its startup commands.
- `environment_startup` and `environment_known`: declarations for known environment variable names and whether those names are fully known.
- `settings`: the result of `configuration_transform`, including `options`.
- `config_file`, `filesystem`, `engine` and `input`: the resolved configuration file, filesystem availability, whether execution uses a snippet engine, and whether the script comes from an action input.

Return `source_prefix` for generated comments, `startup` for generated commands, `config` for tool options,
`variables` for argument substitutions and `excluded` for diagnostic codes. A nonempty `skip` skips the script.
Generated comments must end with a newline. The host inserts comments after a shebang and commands after
leading comments, then maps the inserted lines so they cannot receive edits to the original source.
Omit `config`, or return `null`, when the tool needs no options.

The bundled [ShellCheck transformation](../internal/plugins/manifests/bundled/shellcheck/configuration.jq)
normalizes `enable: true`, writes directive comments and supplies source-directory requests. Its option names
and directive syntax are defined by that integration. Its [preparation program](../internal/plugins/manifests/bundled/shellcheck/preparation.jq)
selects the shell, exclusions and native arguments. The component plugin uses the ShellCheck parser for directives
and requests environment facts to choose its exclusions.

### PSScriptAnalyzer example

The [PSScriptAnalyzer provider](../plugins/examples/psscriptanalyzer/provider.json) checks snippets selected by
`shell: pwsh` and `shell: powershell`. It uses PowerShell 7 to run the installed
[PSScriptAnalyzer](https://learn.microsoft.com/en-us/powershell/utility-modules/psscriptanalyzer/overview)
module, version 1.25.0 or newer. Install the module explicitly; the provider does not install tools during analysis:

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

The provider passes the prepared snippet and settings as JSON on stdin. PowerShell emits diagnostic records;
the inline [jq transform](../plugins/examples/psscriptanalyzer/output.jq), included in the manifest's digest, converts messages, severities
and suggested corrections. The host converts UTF-16 columns and script extents into workflow source locations
and suggested edits. Parse errors remain errors; a missing module or crashed process fails the provider.
Analyzing a `powershell` snippet with this provider uses the installed PowerShell 7 parser; configure
PSScriptAnalyzer compatibility rules when targeting an older PowerShell language version.

The [sample workflow](../plugins/examples/psscriptanalyzer/workflow.yml) deliberately uses an alias in both
shells, including one after an emoji to exercise column conversion. The example has been exercised with
PSScriptAnalyzer 1.25.0. This example is not in the registry.
Load its local provider manifest with `--plugin`.

## Remote artifacts and static metadata

`--plugin` and `tools.<id>.backend.wasm.path`
accept local paths, absolute `file://` URIs, and HTTP(S) URLs; `--plugin` and
`tools.<id>.backend.wasm.path` also accept the `oci://` references described in
[Pull from an OCI registry](#pull-from-an-oci-registry). Both module kinds
accept raw WASM and gzip-compressed artifacts. Relative filesystem paths in
configuration resolve from the declaring file; URIs are preserved.

```sh
actionlint check --plugin 'https://example.com/plugins/ruff.wasm.gz#sha256=<64-hex-digest>'
actionlint plugin inspect 'https://example.com/plugins/ruff.wasm.gz' --json
```

Replace the example URL and digest with your artifact's location and SHA-256.
The `#sha256=` fragment verifies the distributed file **before gzip
decompression**. Remote execution requires this pin; inspection can omit it to discover the digest.
For a `.wasm.gz` artifact, use the checksum of the compressed
file. The CLI caches pinned downloads under the user cache directory's
`actionlint/artifacts` folder and verifies cached bytes on every use. Unpinned
URLs are fetched on each invocation. A corrupted pinned cache entry is an error;
remove that entry to download it again.

Acquisition has a 30-second deadline and a 128 MiB limit on both downloaded and
decompressed bytes. Redirects are bounded and cannot downgrade HTTPS to HTTP.
HTTP is supported explicitly, including local development servers. URI userinfo is rejected; query strings are
omitted from reported artifact locations.

To inspect a registry plugin, use its alias or package name, with an optional
version:

```sh
actionlint plugin inspect shellcheck
actionlint plugin inspect github.com/kjanat/actionlint-plugin-shellcheck@v0.3.1 --json
```

Without a version, actionlint selects the registry's latest release. Use
`--registry URL` to select another registry. Inspection downloads and verifies
the plugin, then displays its metadata without executing it or changing your
configuration.

The `actionlint.plugin` custom section stores JSON metadata inside the Wasm file.
`actionlint plugin inspect` reads this section and the binary header without
compiling or running the plugin. It reports the binary kind, checksum, downloaded
and decoded sizes, and all metadata fields supplied by the plugin. These can
include its name, rule descriptions, configuration schema and supported interfaces.
actionlint reads this section only in the outermost module or component and ignores
metadata inside nested modules. It rejects
duplicate sections, duplicate JSON keys, invalid UTF-8 and metadata exceeding 1 MiB.

When loading a core script engine, actionlint checks the section's ABI version and
transport before compilation. It then calls the engine's `metadata()` function
and requires the returned JSON to match the section. Whitespace and object key
order can differ. If the section is absent, actionlint reads metadata from that
function alone.

When loading a component analyzer, actionlint checks its imported and exported
WIT interfaces and calls its `describe` function to obtain the rule name,
description and settings schema. If the custom section contains `invocation_definitions`,
actionlint also reads and validates those shell command definitions for script checks.
`actionlint plugin inspect` displays all JSON metadata embedded in that section.

## Contribute a rule

Plugins can live in their own repositories. Choose the contract that fits the integration:

| Contribution                             | Public contract                                         | Authoring path                                                                          |
| ---------------------------------------- | ------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| Workflow analyzer or runner provider     | Versioned WIT interfaces                                | [Component examples](#write-a-plugin)                                                   |
| Embedded script engine                   | Versioned WIT or core Wasm JSON ABI                     | [Script engines](#script-engines)                                                       |
| External executable or container scanner | Version 1 process-provider manifest and diagnostic JSON | [TypeScript SDK](../plugins/sdk/typescript/README.md), including jq tests and packaging |

Plugins use the same interfaces regardless of the host's implementation
language. Target a supported interface version; incompatible changes require a
new version. Plugins communicate through these interfaces. Host implementation
packages are private. The SDK currently provides local tarballs and package
previews; follow its README to obtain a matching host CLI.

Publish through the [registry submission workflow](https://github.com/kjanat/actionlint-registry/blob/master/docs/publishing.md).
Registry validation checks publisher identity, metadata and artifact digests. It does not certify plugin safety or compatibility
with every host. Run the candidate fixture against the exact artifact and intended host version. Native process
providers also require the user's execution opt-in. Open an issue here to add a plugin to the documentation.

Built-in rule declarations, implementations and tests live in their category under
[`internal/checking/rules`](../internal/checking/rules). The index in
[`internal/checking/rule_index.go`](../internal/checking/rule_index.go) reads those
declarations; `internal/input/configuration` maps rule declarations to configuration settings.
Workflow adapters in `internal/checking/executor/declarations` supply inputs to
the checks. The index generates [`docs/rules.md`](rules.md);
[`docs/checks.md`](checks.md) provides behavior examples. External plugins use the
[plugin SDKs and delivery contracts](api.md#write-and-test-a-plugin).

## Source positions and fixes

actionlint captures source before analysis. Coordinate conventions depend on the
interface: analyzer WIT positions use one-based Unicode character columns and
exclusive range ends. Process-provider manifests can specify the tool's row,
column, and end conventions. If they omit `coordinates`, actionlint assumes LF
rows, Unicode character columns, and exclusive ends, then converts those
positions to the analyzer convention. TypeScript SDK fixes also use one-based
Unicode character columns and exclusive ends. Rule descriptors record category,
subcategory, maturity, and provider as separate fields.

Revision requirements also depend on the interface. Analyzer WIT fixes include a
digest for each edited document. TypeScript and process-provider fixes have no
revision field. actionlint records the current source revision for an edit only
when an edit targets the document that produced the finding. Fix application
checks the recorded revisions and ranges against current files.

WIT 0.2 related locations contain a path and range but no source revision or
coordinate format. actionlint adds the current request's revision and coordinate format only
when the related path exactly matches `Input.Path`. WIT 0.2 cannot carry those
facts for a different document, so actionlint omits cross-document related
locations from that interface. Interfaces with revision and coordinate
fields can report locations in other documents. actionlint checks document
identity, source digest, coordinate format, display path, and range against the
captured text before reporting a location. It omits locations that fail these
checks, keeps the main diagnostic, and reports the number omitted in an
`admission-rejected` error. That error marks the analysis incomplete. Native
analyzers follow the same checks. Adding revision and coordinate fields to WIT
0.3 or JSON2 requires a versioned interface change; WIT 0.2 keeps its current
fields.

Plugins requesting `dependencies` receive literal action and reusable-workflow
references, including source ranges and the identities of their declarations.
Dynamic references are omitted.

Script engines analyze sanitized snippets through either the typed component interface or the version 1 JSON ABI.
`--plugin` accepts either binary kind and selects the appropriate host from its
WebAssembly header. A failing component is never retried as a core module.

The Ruff plugin is a script engine. It uses `tools.ruff` settings and the same
expression sanitizer and YAML position mapper as native Ruff. Selecting it replaces
the default Ruff executable; an explicitly configured competing backend is rejected.

Inspect selected plugin metadata with `actionlint rules --plugin PATH --json`, or
`actionlint rules RULE --plugin PATH` for its explanation. Duplicate document-analyzer
rule names are rejected before analysis.

## Configure registered rules

Loading a document analyzer explicitly enables it by default. Configure it under
`lint.rules.plugins`. Third-party rules share this configuration namespace. Their
semantic categories, such as security, are recorded in plugin metadata:

```yaml
lint:
  rules:
    plugins:
      no-sudo: warn
      organization-check:
        level: error
        options:
          required-prefix: team-
```

Names must match analyzers registered by this invocation. Global/group presets,
`lint.enabled`, `lint.rules.disable` and per-file overrides apply to plugin rules.
An explicit `recommended` preset uses the descriptor's recommendation; `all`
excludes rules whose metadata sets `maturity` to `nursery`. The `--experimental`
flag enables those rules. Disabled analyzers are not executed. Options are checked
against the descriptor's bundled JSON schema before execution; external schema
references are refused. Go registration options override repository options.
Core script engines with a bundled adapter use that adapter's configuration (for example,
`tools.ruff`). Independent script engines use `lint.rules.plugins.<engine.name>` for
options and severity. They advertise `capabilities.script_shells`, for example
`["python", "python3"]`, in their version 1 metadata. The host selects snippets by
these shell names and registers the engine's rule descriptor in the catalogue.
Local paths and remote artifact URLs use the same registration path.

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

Selectors use lowercase owner/repository names and case-sensitive subpaths without `@ref`, an input name and an explicit language. Targets use exact
matching; wildcard selectors are rejected. Duplicate targets, conflicting languages and malformed selectors are
rejected. Up to 256 selectors are allowed per engine. These declarations route snippets; they do not prove the
guest understands a language. Input-only components may return an empty `shells` list from `describe`, but must
provide valid input selectors in their root `actionlint.plugin` section. The host's
[`action-input.metadata.json` test fixture](../internal/plugins/execution/component/testdata/sources/script-engine/action-input.metadata.json)
and `mise run plugin:fixture:action-input` exercise metadata at the component root.
Metadata inside embedded core modules is ignored.

The host recognizes `actions/github-script`'s `script` input as JavaScript. Other sites come from registered engine
declarations. Input requests use the virtual filename
`actionlint-input/<language>/<owner>/<repository>[/<subpath>]/<input>.<extension>`.
Language and action/input segments come from the validated selector; Python, JavaScript
and TypeScript use `py`, `js` and `ts` extensions; other languages use their own name.
Guests can distinguish sites and select a parser without filesystem access or a new
wire interface.
Configured Wasm backends use the same selectors as explicitly loaded plugins, and
disabled engines do not contribute selector conflicts for that effective configuration.
Workflow and local composite inputs carry source maps and invocation identity;
workflow expressions are masked before checking. Whole inputs created by object expressions have no physical
script scalar and cannot be analyzed through this interface. Ordinary action inputs and `run` scripts are not sent to
an input-only engine. Action inputs have a language, never an inferred runner shell, and cannot opt into ShellCheck
through a shell directive.

Engines replacing Ruff or ShellCheck receive the corresponding `tools.ruff` or `tools.shellcheck` settings for both
run scripts and declared inputs, including file overrides. ShellCheck's inline directive positions map to the input scalar.

Go document analyzers requesting `analyzer.Scripts` receive these sites separately in `Input.ActionInputs`, with
`Site` and spans. Request `analyzer.Invocations` for `Occurrence` and its ancestry in `Input.CallGraph`.
Resolve the occurrence owner through `CallGraph.Call`, or `CallGraph.Entry` for entry-owned inputs.
Shell and runtime fields are empty for action inputs. `Input.Scripts`
and `Input.Invoked`, including the existing portable analyzer WIT, continue to contain only `run` scripts.

The host supplies prepared snippets, source maps and execution settings to each
registered tool runner. The runner returns findings with their originating
snippet index. Native process providers can use the same JSON request/result
protocol as core WASM: a `ScriptRequest` on stdin and an object containing `ok`,
`diagnostics` and `errors` on stdout. A nonzero process exit is an execution failure.
Native CLIs with their own protocols require an adapter; their argument construction,
configuration and diagnostic decoding belong to that adapter.

The checking engine and application receive registered tools from their caller.
They neither import the bundled Ruff and ShellCheck adapters nor choose behavior
by matching those tool names. The adapters handle each language's expressions
and map findings back to source positions.
