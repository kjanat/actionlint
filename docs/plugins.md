# Plugins

actionlint runs WebAssembly components that implement the `kjanat:actionlint/analyzer@0.1.0` interface. The contract is
[`crates/actionlint-extension-api/wit/analyzer.wit`](../crates/actionlint-extension-api/wit/analyzer.wit). A plugin describes itself once, then receives one document per
call with the facts it declared and returns diagnostics. Components may instead implement the independent
`kjanat:actionlint/runner-provider@0.1.0` interface or the
[`kjanat:actionlint/script-engine@0.1.0`](../crates/actionlint-extension-api/wit/script-engine.wit) interface. Components can implement multiple
roles. Any language with a component toolchain can implement them.

Tool integrations are discovered through the optional
[`tool-integration`](../crates/actionlint-extension-api/wit/tool-integration.wit) export,
regardless of repository name, registry alias or loading path. A conforming fork receives
the same configuration, suppression and execution behavior. The plugin negotiates the
versioned host configuration profile together with opaque caller options before execution;
it declares resource requests and configurations it cannot support. Native fallback requires
the caller to have enabled native execution.

The [ShellCheck profile](../crates/actionlint-extension-api/schemas/shellcheck-config-0.1.0.md)
uses typed `source-files` mount translations and `prepared-scripts` source preparation.
Shell interpretation, masking, startup preparation and exclusions have one host implementation.
Managed filesystem grants stay within the host-selected workspace. Configured
source paths and symlink targets that resolve outside it fail analysis; missing paths receive no grant.
Plugin options do not carry filesystem mount metadata. The
[`template-effects`](../crates/actionlint-extension-api/wit/template-effects.wit) interface
classifies snippet diagnostics for substitution-aware filtering; hosts never infer these
effects from diagnostic wording or rule codes. Structural WIT types determine compatibility;
source-file hashes are informational build provenance.

The alternative [core JSON memory ABI and versioned operations](../crates/actionlint-extension-api/wit/core-json-abi.md)
are specified alongside WIT. Check operation 2 carries the same template-effect semantics.

## Install from the registry

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
registered GitHub release or GitLab generic package. Registry publication remains a separate publisher workflow.

Installation updates the root `plugins` list in your existing `.github/actionlint.yaml` (also `.yml`, `.json`
or `.toml`), creating `.github/actionlint.yaml` when no config exists:

```yaml
plugins:
  - github.com/owner/plugin@v1.2.3
  - gitlab.com/group/plugin
```

The registry explicitly assigns short names such as `shellcheck` and `ruff` to published plugins.
Names are never inferred from a repository prefix or claimed through plugin metadata.
The config retains the shorthand; the lock records that alias and the full resolved repository identity.
Aliases belong to their selected registry. Checking uses the recorded identity offline;
an update refuses an alias reassigned to a different repository.
Plugins without an assigned alias use `owner/plugin`, `github.com/owner/plugin` or `gitlab.com/group/plugin`.
A new dependency without `@version` selects `latest`.
Reinstalling an existing dependency preserves its declared version and registry unless explicitly overridden.
Use an object for a custom registry:

```yaml
plugins:
  - name: github.com/owner/plugin
    version: v1.2.3
    registry: https://example.org/catalogue/
```

Exact downloaded versions and verification data go into the adjacent `actionlint.plugins.lock.json`.
Commit **both files**. The config declares dependencies; the lock records their resolved artifacts.
Each lock entry records publisher identity, exact release tag, artifact URL, kind, interface version and SHA256.
The digest covers the distributed bytes, including compression.
Install verifies the download and embedded metadata without executing guest code. Actual ABI/capability checks
run when the plugin loads. Registry admission and reports for another host revision do not guarantee compatibility.
`versions --json` includes the registry's host-specific compatibility evidence; missing evidence is not a failure.

For new dependencies, omitting `--version`, or using `--version latest`, selects the highest stable full semantic version. Prereleases
and non-semantic tags require exact selection. Equal-precedence tags are ambiguous and require an exact tag too.
Exact tags remain exact during updates. To change an installed selector:

```sh
actionlint plugin update github.com/owner/plugin --version latest
actionlint plugin update
actionlint plugin sync
actionlint plugin remove github.com/owner/plugin
```

Only install/update resolve selectors against the catalogue. `sync` restores the exact locked artifacts into
the shared user cache and refreshes registry withdrawal information, useful after cloning or in CI.
Checks also restore missing pinned artifacts; populated
caches work offline and cached bytes are re-hashed. No implicit upgrades run during linting or LSP analysis.
`latest` refuses downgrades from a stale catalogue; an explicit exact tag permits a deliberate downgrade.
Failed downloads, identity changes, same-version digest replacements and concurrent lock edits preserve the old
lock. Updates acquire every selected artifact before saving config and lock. A failed config write restores
the previous lock unless another writer changed it. Removal updates both files and leaves shared cache entries intact.

A new release may change its artifact filename or migrate from a core module to a component.
An explicit update records that release's format, filename, interface and digest. Publisher, repository
and plugin identity must remain unchanged, and an existing release cannot be replaced with different bytes,
format or filename. Checking, synchronization and installation with an already matching pin keep the old artifact.

Edit `plugins` directly, then run `actionlint plugin install` without arguments to resolve missing or changed
dependencies. Existing matching pins stay pinned. This command also migrates older lock-only installations
into config without upgrading their artifacts. Checks require declarations and matching pins; they never
activate extra entries left in the lock. `plugins: []` or `plugins: null` disables all declared plugins,
including inherited ones. An inherited list is replaced as a whole; install/remove edit only the selected leaf config.

`actionlint config init` installs and pins `shellcheck` and `ruff`. Migrating an old configuration
adds these defaults when `plugins` was omitted, respecting disabled checks and explicit native backends.
All downloads must verify before the migrated config and lock are saved.
See [configuration migration](config-migration.md) for preview and automatic migration behavior.

### Try an unpublished candidate

A local candidate catalogue installs packaged bytes before creating a release or registry submission:

```sh
actionlint plugin install owner/plugin@v1.2.3 --registry file:///absolute/path/to/candidate/
actionlint check
```

On Windows, use `file:///C:/path/to/candidate/`. The registry's candidate exporter creates this directory
from a packaged artifact. Installation creates the usual configuration and lockfile, verifies the artifact's
digest and metadata, and imports it into the shared cache. The lock identifies the source as `candidate`;
it makes no claim about publisher authentication or publication. Candidate catalogues are accepted only through
an explicit local file URI. Their records and artifacts must stay within that directory.

Checking uses the imported bytes even after the candidate directory is removed. A missing cache entry fails;
`plugin sync` can restore it from the local catalogue. Use `plugin update owner/plugin` after rebuilding a candidate
at the same version. Published versions still reject replacement. Run candidate checks in a temporary project
to keep local paths out of a project's production configuration.

### Deprecated, withdrawn and revoked releases

Registry status is separate from immutable release records. Deprecated versions remain installable
with a warning. Withdrawn versions are excluded from new resolution; an existing lock can continue
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

Direct declarations already contain their exact artifact pin, so they need no lock row; projects
using only direct URLs do not create a lockfile. Changing the artifact requires changing its URL
or digest explicitly. Registry withdrawal information does not cover independent direct URLs.
Remote execution through `--plugin` or configured Wasm backends also requires a digest pin.

### GitHub Action caching

The JavaScript Action loads the same declarations and locked artifacts as the CLI.
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
options take precedence. Components without a tool-integration export remain standalone analyzers.

Check, fix, rules and LSP load declared plugins from their matching locked artifacts. Selection precedence:

- Explicit `--plugin` sources replace automatic selection.
- `--plugin-lock FILE` explicitly loads every entry in one invocation-wide lock, bypassing config declarations.
  It cannot accompany `--plugin`.
- `--no-config` (or `ACTIONLINT_NO_CONFIG`) disables automatic plugin selection; explicit sources/locks still work.
- Explicit `--config FILE` selects that config's declarations and adjacent lock, with no repository fallback.
- Otherwise declarations come from the nearest repository's `.github/actionlint.*`, with its adjacent lock.

Checks spanning repositories with a lock require separate invocations or an explicit invocation-wide selection.
LSP snapshots the startup selection; restart after installing, updating or removing plugins. Automatic project
plugins cannot apply to documents from another repository. Use a separate server or an explicit lock for that scope.
Installing a runner provider does not select its execution target; `--runner-target` remains explicit.
Management commands accept `--config FILE` too. Outside a Git repository, select a config with `--config FILE`,
or use `--plugin-lock FILE` to place a config beside that lock. Management commands reject `--no-config`.

## Load a plugin directly

```sh
actionlint --plugin ./pipe-to-shell.wasm --plugin ./no-sudo.wasm
```

Each `--plugin` loads one component or a supported core script engine. Analyzer findings carry the plugin name as their rule, so `--ignore-regex`, inline
suppressions and configured ignores apply to them like built-in rules. `--log-level debug` prints the plugin's limits
when it loads and forwards its log lines, stdout and stderr.

Pass the same `--plugin` arguments to `config show`, `config validate` or `doctor` when the configuration names plugin
rules. These commands load the descriptors to validate rule IDs without running document analysis; unknown rules
remain errors.

The language server retains loaded plugins until shutdown. Explicit `--plugin` selections and remote engine URLs
use a fixed snapshot for that session; restart to refresh them. Configured local engine files reload when their
content changes, and selecting a different engine URI loads that identity separately. Earlier engines remain
available to active analyses until shutdown. Every analysis still receives fresh document buffers and invocation state.

The host refuses a component at load when it exports an unsupported version of a recognized interface, imports an interface
the host does not grant, or exports no supported role. Before guest startup it checks each implemented interface's complete
type graph against the canonical WIT, including function signatures, record fields and enum ordering. The expected
graph and enum discriminants are generated with `go generate ./internal/plugins/component`; a test detects stale output.

## Limits and capabilities

Every analysis call runs in a fresh instance with a shared 64 MiB limit across live linear memory and reserved core
tables in its plugin runtime. Its 10-second timeout starts after queue
admission and covers instance setup and guest execution. Queue waiting honors the caller's cancellation and
deadline; it has no separate timeout. A trap, a panic, an exit, an
exhausted limit or a cancelled run fails the analysis of that document with an error that names the plugin and its
limits. The run then continues with the next document and actionlint exits with status 3.

Component results and host-import arguments also have per-call budgets of 262,144 values and 8 MiB of accounted
string and collection storage. Nested values and repeated aliases count toward these budgets; validation runs
before host allocations and string encoding conversions.
The pinned [`kjanat/wacogo`](https://github.com/kjanat/wacogo/tree/v0.1.0)
runtime adds these allocation checks to wacogo.

Stdout, stderr and the direct `host.log` capability share a 1 MiB output budget per call, including when debug logging is disabled. Exceeding it fails
the analysis even if the plugin handles the write error. Log lines are emitted in chunks of at most 64 KiB, and an
unfinished line is flushed at the end of its call.

The host grants these imports and nothing else:

| Interface                        | What the plugin gets                                                                        |
| -------------------------------- | ------------------------------------------------------------------------------------------- |
| `kjanat:actionlint/host@0.1.0`   | `log`, one line into actionlint's debug log                                                 |
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

The public Go package `actionlint.kjanat.dev/v2/plugin` provides `plugin.Load`, `plugin.Options` and `plugin.Limits`.

## Runner providers

[`crates/actionlint-extension-api/wit/runner.wit`](../crates/actionlint-extension-api/wit/runner.wit) defines the separate `runner-plugin` world. A provider-only
component exports `describe` and `resolve`; it needs no dummy analyzer. Existing analyzer components remain compatible.
The host resolves a batch of whole selectors before running checks. Labels within a selector are conjunctive; groups,
correlated matrix alternatives, source positions and unresolved expressions survive normalization. Incomplete matrix
alternatives cannot establish a resolved execution environment.

Runner validation, shell validation, script execution facts, local action resolution and executable checks consume
the same prepared knowledge. The built-in GitHub catalogue implements this boundary too. Installing a provider never
changes authority: `--runner-target` defaults to `github`; choose another target explicitly. Multiple applicable
providers require `--runner-provider`. Configuration labels stay configuration, without plugin mutation.

Results distinguish resolved candidates, no match, unknown and not applicable. Operational errors remain separate;
independent checks still run, while runner-dependent inference stays unknown. No match produces a `runner-label`
finding only with fresh, complete evidence. Stale, missing or incomplete evidence cannot prove an invalid label or a default shell.
Explicit shells and container defaults retain precedence. Providers contribute knowledge; the host controls diagnostic policy.

Configured self-hosted labels remain an allowlist. A literal label matching that list with `windows` as a complete,
case-insensitive hyphen-separated segment supplies Windows and the `pwsh` default, including vendor-prefixed names.
This convention does not accept an unconfigured label or infer an OS from arbitrary substrings. Contradictory OS
labels produce a runner conflict and leave execution facts unknown.

For custom environments that do not follow that convention, select a runner provider and supply its snapshot.
The provider returns each candidate's explicit `os` and `default-shell`; these shared facts drive shell validation,
script analysis and path interpretation. The snapshot format belongs to that provider, as the queue example below
illustrates. The built-in GitHub provider does not interpret another provider's snapshot. A runner's OS does not
select a script linter: script engines declare the shells they analyze. Missing or stale provider evidence leaves
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
Omitted facts remain unknown; declaring Windows alone does not assert that PowerShell is installed.
Unknown JSON fields and invalid enum values are errors. An empty `runners` array asserts no eligible runners
only when the snapshot is fresh and complete. Missing, stale or incomplete evidence cannot establish a default shell
or reject a selector. Freshness and completeness are assertions supplied by whoever maintains the inventory.

Selection is explicit: supplying a snapshot alone does not replace the default `github` provider. The `snapshot`
provider uses only the inventory, without adding hosted labels or configuration allowlists. Include every eligible
candidate relevant to the workflows being checked before marking the inventory complete. These flags also work with
`actionlint lsp`; restart it after updating the snapshot. Go callers use `runner.Options{Provider: "snapshot", ...}`.

For this inventory, `runs-on: desktop-pool` supplies Windows and `pwsh` to runner checks, shell inference and script
routing. Load a script-engine plugin advertising `capabilities.script_shells: ["pwsh", "powershell"]` to analyze
PowerShell snippets. The host selects engines by the effective shell; no `script-linter` setting is needed on a runner.
This does not install a PowerShell analyzer. Without a matching engine, those snippets receive no engine diagnostics.
ShellCheck handles an explicit `shell: bash` override. Step, job and workflow
shell declarations override the inventory; a job container supplies its own `sh` default.

### Offline Buildkite example

[`examples/plugins/buildkite-runners.rs`](../examples/plugins/buildkite-runners.rs) resolves an explicit queue mapping
snapshot. This offline example uses supplied queue data without live Agent API calls or guarantees of GitHub image/toolchain parity.
Each queue declares its name, labels, optional group, OS, architecture and default shell. All labels and the requested
group must match the same queue. The catalogue's completeness refers to the selected scope.

```sh
cargo build --locked --profile wasm --target wasm32-wasip2 --example buildkite-runners
actionlint --plugin target/wasm32-wasip2/wasm/examples/buildkite_runners.wasm \
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

The host reads this bounded envelope once per session. Refresh by replacing the snapshot before another CLI run,
or restarting the language server. Freshness is an explicit input classification; no implicit TTL or network refresh
runs during analysis. Snapshot and answer scope must match the requested scope. Host snapshot limits on freshness
and completeness cannot be upgraded by a provider.

The public `runner` package exposes the provider contract and `analysis.Options.Runners` selects it. Loaded components
expose `HasAnalyzer`, `HasRunnerProvider`, `RunnerDescriptor` and `ArtifactDigest`. Register providers in
`runner.Options.Providers`. `analysis.Result.RunnerEvidence` records provider/artifact identity, target, scope,
snapshot digest and answers. Its dependency identity includes the request, implementation, configuration and returned
evidence; consumers caching findings must include it. Changing a catalogue or artifact invalidates that identity.
Provider trust is selected by the host; the Wasm sandbox does not prove answers true.

### Cross-runtime verification

[`tools/runner-host`](../tools/runner-host) is a finite Wasmtime parity probe scoped to the runner-provider interface.
It uses the unchanged WIT and the exact same component bytes as the Go host. Build and compare normalized answers,
source positions, candidate guarantees, evidence and artifact digest:

```sh
cargo build --locked --manifest-path tools/runner-host/Cargo.toml --target-dir target/runner-host
ACTIONLINT_RUNNER_HOST="$PWD/target/runner-host/debug/actionlint-runner-host" \
  go test ./internal/plugins/component -run TestRunnerProviderWasmtimeParity -count=1
```

On Windows, set `ACTIONLINT_RUNNER_HOST` to the absolute `.exe` path before running the test.

## Write a plugin

The world is `plugin`: it imports `host` and exports `analyzer`. `describe` returns the plugin name and the facts it
needs. `analyze` receives the document path and content, declared scripts, scripts invoked through composite actions,
mapping names and the JSON options the host registered, and returns an `outcome` containing diagnostics and an optional
error message. Diagnostics remain available when the error marks analysis incomplete. A trap cannot return findings.

Script text comes with spans that map each line and column back to the declaring document. The WIT comment on `span`
gives the lookup. A diagnostic `rule` must be empty or equal the plugin name; use `code` for check identifiers.
Findings with other rule names are rejected as incomplete analysis while valid findings remain available.
An empty `rule` takes the plugin name, and an empty `path` means the analyzed document, so findings in a composite
action copy `script.path`.

Plugins can opt into the [`source-annotations`](../crates/actionlint-extension-api/wit/source-annotations.wit) interface by selecting the
`annotated-plugin` world. The existing `analyzer@0.1.0` interface and its contract digest are unchanged. The optional
host function `comments` accepts a complete script source reference and returns physical YAML comments attached
immediately before its declaration or after its scalar/header, with placement and Unicode source positions.
Comments inside a scalar remain part of `script.text`; attached YAML comments never change that text or its spans.
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
block body is shell text and retains ShellCheck's placement error. Errors in attached directives point to the YAML
comment; script diagnostics and fixes keep their original scalar spans and document revisions.

### Rust

[`examples/plugins/pipe-to-shell.rs`](../examples/plugins/pipe-to-shell.rs) flags `curl ... | sh` patterns. It depends on
[`actionlint-extension-api`](../crates/actionlint-extension-api), which provides the generated guest types,
`Guest` trait, host imports and `export_plugin!` macro. Its [README](../crates/actionlint-extension-api/README.md)
contains a minimal analyzer implementation. The example builds with:

```sh
cargo build --locked --profile wasm --target wasm32-wasip2 --example pipe-to-shell
```

The component is `target/wasm32-wasip2/wasm/examples/pipe_to_shell.wasm` at the repository root.

Enable the SDK's `source-annotations` feature to opt into the additional comment import. The default feature set
continues to use the original analyzer world. The SDK's native `package` feature provides `package::Package`:
pass the plugin's descriptor, Cargo identity, engine, capabilities, languages and license text, then call `write`
with the component path and output directory. The SDK writes discovery metadata, schema and license sidecars,
and checksums. Plugins do not implement their own Wasm section traversal or vendor the SDK's WIT contracts.

### Shared declaration facts

[`examples/plugins/pinned-actions.rs`](../examples/plugins/pinned-actions.rs) and its
[`native Go counterpart`](../examples/plugins/pinnedactions/pinned_actions.go) request only `dependencies`.
Both flag remote action and reusable-workflow references that do not use a full commit hash, while excluding
local references and Docker images. They use the host's parsed `uses` values, dependency kinds, source identities
and scalar ranges; neither reparses the workflow or reads `input.content`.

```sh
cargo build --locked --profile wasm --target wasm32-wasip2 --example pinned-actions
```

The component is `target/wasm32-wasip2/wasm/examples/pinned_actions.wasm`.
`TestDependencyAnalyzerNativeComponentParity` checks identical findings from both implementations with empty,
invalid and unrelated source content. `TestDependencyAnalyzerSharedDeclarationFacts` checks the complete path
from workflow parsing to diagnostics, including quoted scalar ranges and revision-bound node references.
The example validates declaration facts already present in the contract; other checks may require additional facts.

### C

[`examples/plugins/no-sudo`](../examples/plugins/no-sudo) flags `sudo` at the start of a command. `build.sh` generates
bindings with `wit-bindgen c` and compiles with wasi-sdk:

```sh
WASI_SDK_PATH=/opt/wasi-sdk ./build.sh no-sudo.wasm
```

### TinyGo

[`examples/plugins/insecure-download`](../examples/plugins/insecure-download) flags `curl -k` and
`wget --no-check-certificate`. It is a Go module that depends on `go.bytecodealliance.org/cm`, with `wit-bindgen-go`
pinned as a Go tool. TinyGo needs the WASI imports in the compiled world, so `build.sh` composes a world from the
contract and TinyGo's bundled WASI definitions, then builds with:

```sh
./build.sh insecure-download.wasm
```

TinyGo's `cabi_realloc` returns null for a zero-size request, which the canonical ABI allows. wacogo accepts it since
https://github.com/partite-ai/wacogo/pull/12. The pinned runtime includes this fix.

### Other toolchains

Any toolchain that emits a component for the `plugin` world works.

### Build the examples

Run `make plugin-examples` from the repository root to build the Rust, C and TinyGo examples.
The Rust components are Cargo `[[example]]` targets. The C and TinyGo builds use their language toolchains.
Tests load the components through the plugin interface.

For an independent Rust analyzer, create a package with its own manifest and lockfile and depend on the SDK.
Other languages can vendor the public [`crates/actionlint-extension-api/wit`](../crates/actionlint-extension-api/wit) contracts and point their binding generator
at that copy. The host's fixture workspace is not a dependency of external plugins.

## Test a plugin

Run it against a workflow:

```sh
actionlint --no-config --plugin ./plugin.wasm .github/workflows/ci.yml
```

From Go, load it with `plugin.Load` from `actionlint.kjanat.dev/v2/plugin`, wrap it in `analyzer.Registration` and pass it
in `analysis.Options.Analyzers`. [`plugin/example_test.go`](../plugin/example_test.go) demonstrates the public API.
The internal integration tests also cover composite action calls. When parsing configuration yourself, pass the
registered analyzer names to `config.Parse`, `config.Load` or `config.ParseOverlay` so suppression names can be validated. File analysis and
the CLI derive those names from the registered analyzers automatically. The Rust crate under
`testdata/plugins/misbehave` shows how traps, panics, exits, allocation beyond the limit, hangs and denied
file access are reported.

## Script engines

Python and shell snippets use `analyzer.ScriptBackend`: actionlint selects the snippet,
masks workflow expressions and maps positions back, and the engine checks one sanitized snippet at a time.
The preferred component interface is [`script-engine.wit`](../crates/actionlint-extension-api/wit/script-engine.wit), world
`script-engine-plugin`. It has typed `describe`, `validate-config` and `check` operations. Plugin-specific
configuration remains a JSON object checked by the engine; shared descriptors, diagnostics and outcomes use WIT
records. Coordinates count one-based Unicode scalar values, with exclusive range ends. An outcome can contain
both findings and an incomplete-analysis error. This interface does not provide fix edits.

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

From Go, load a component with `plugin.Load`, require `HasScriptEngine()`, and set the component as
`analysis.Tool.Backend`. `plugin.LoadEngine` continues to load core modules unchanged. Component loading checks the
typed interface, runtime descriptor and static declaration before registering the engine. Component engines use
the same component execution limits and lifecycle as the other component roles.

Legacy core modules retain their version 1 JSON ABI: `check`, `validate_config` and `metadata` exports, no imports,
and UTF-8 JSON in linear memory. The host checks the ABI version, export signatures and advertised operation versions,
refuses modules with imports, and runs each call in a pooled instance under a 256 MiB memory limit and a 10 second time
limit. Defined tables require finite maxima; conservative reservations for their maximum size and growth buffers share
that memory budget with linear memory. Components charge each core instance, including repeated and nested modules;
imported table aliases are charged to their defining instance only. Artifact acquisition defaults to 128 MiB for both raw and gzip-expanded bytes. Every response, including metadata
and configuration validation, must fit within 8 MiB before the host reads, copies or decodes it.
Initialization and creation of pooled instances use the same time limit. Native compilation has a separate
one-minute deadline; a cold compilation does not consume the guest execution budget. An earlier caller deadline
still applies to both phases. Compiler cancellation is cooperative, and an expired compilation never proceeds
to guest initialization. A trap or an exceeded limit, including
during allocation cleanup, discards that instance and fails the document's analysis. From Go, call
`plugin.LoadEngine` and set the result as `analysis.Tool.Backend`; library tool settings take precedence over the
repository configuration.

## Remote artifacts and static metadata

`--plugin`, `tools.ruff.backend.wasm.path`, `plugin.Load`, and `plugin.LoadEngine`
accept local paths, absolute `file://` URIs, and HTTP(S) URLs. Both module kinds
accept raw WASM and gzip-compressed artifacts. Relative filesystem paths in
configuration resolve from the declaring file; URIs are preserved.

```sh
actionlint check --plugin 'https://example.com/plugins/ruff.wasm.gz#sha256=<64-hex-digest>'
actionlint plugin inspect 'https://example.com/plugins/ruff.wasm.gz' --json
```

Replace the example URL and digest with your artifact's location and SHA-256.
The optional `#sha256=` fragment verifies the distributed file **before gzip
decompression**. For a `.wasm.gz` artifact, use the checksum of the compressed
file. The CLI caches pinned downloads under the user cache directory's
`actionlint/artifacts` folder and verifies cached bytes on every use. Unpinned
URLs are fetched on each invocation. A corrupted pinned cache entry is an error;
remove that entry to download it again. Library callers opt into caching through
`Options.Artifact.CacheDir` or `EngineOptions.Artifact.CacheDir`.

Acquisition has a 30-second deadline and a 128 MiB limit on both downloaded and
decompressed bytes. Redirects are bounded and cannot downgrade HTTPS to HTTP.
HTTP is supported explicitly, including local development servers. Library
callers can set acquisition limits through `ArtifactOptions`, independently of
the runtime's execution limits. URI userinfo is rejected; query strings are
omitted from reported artifact locations.

`actionlint plugin inspect` and Go's `plugin.Inspect` read the binary header and
the top-level `actionlint.plugin` custom section without compiling or executing
the plugin. Inspection reports the binary kind, distributed checksum and size,
decoded size, and the section's JSON metadata. It preserves the complete metadata
object, including engine identity, rule descriptors, configuration schema, and
advertised interfaces when supplied by the artifact. Nested modules cannot supply
metadata for their enclosing component. Duplicate sections, duplicate JSON keys,
invalid UTF-8, and metadata exceeding 1 MiB are rejected.

Static metadata is a declaration; successful inspection does not establish that
the plugin's code or ABI is valid. The selected runtime still checks the module.
For core script engines, an incompatible ABI or transport in the custom section
is rejected before compilation, and the runtime `metadata()` response must agree
with the section, allowing differences in whitespace and object key order.
Older engines without the section retain runtime metadata discovery. Component
analyzers retain their WIT `describe` contract; their custom section is exposed
for inspection but does not replace that contract.

## Contribute a rule

Plugins live in their own repositories and ship a component. Open an issue in this repository to list a plugin in the
documentation.

Built-in rule metadata lives in `internal/rulecatalog`; configuration projections live in
`internal/config`. Stateless checks in `internal/semantics` accept explicit inputs and return diagnostics.
The remaining visitor implementations in `internal/engine` are migration adapters. New document analyzers implement
`analyzer.Analyzer` as described in the [Go API](api.md#add-an-analyzer). The catalogue generates
[`docs/rules.md`](rules.md); behavior examples remain in [`docs/checks.md`](checks.md). Changes to the WIT contract keep `kjanat:actionlint/analyzer@0.1.0`
usable and add a new version for incompatible changes.

## Rule and source contracts

Documents, scripts, invocations and mappings carry source references. Diagnostics
carry structured help, related locations and revision-bound fixes. Descriptors are
shared rule metadata with independent category, subcategory, maturity and provider.
Plugins requiring `dependencies` receive literal action and reusable-workflow
references, including source ranges and owning node identities. Dynamic references
remain unknown and are not sent as literal dependencies.

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
semantic categories, such as security, remain plugin metadata:

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
excludes nursery rules. Disabled analyzers are not executed. Options are checked
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
provide valid input selectors in their root `actionlint.plugin` section. The existing script-engine WIT and core
ABI remain unchanged. [`action-input.metadata.json`](../examples/plugins/action-input.metadata.json) and its
Makefile target demonstrate packaging metadata at the component root. Metadata inside embedded core modules is
ignored.

The host recognizes `actions/github-script`'s `script` input as JavaScript. Other sites come from registered engine
declarations. Input requests use the virtual filename
`actionlint-input/<language>/<owner>/<repository>[/<subpath>]/<input>.<extension>`.
Language and action/input segments retain the validated selector; Python, JavaScript
and TypeScript use `py`, `js` and `ts` extensions; other languages use their own name.
Guests can distinguish sites and select a parser without filesystem access or a new
wire interface. Ordinary run requests retain their existing adapter filenames.
Configured Wasm backends use the same selectors as explicitly loaded plugins, and
disabled engines do not contribute selector conflicts for that effective configuration.
Workflow and local composite inputs retain their declaring source maps and invocation identity;
workflow expressions are masked before checking. Whole inputs created by object expressions have no physical
script scalar and remain unavailable for this analysis. Ordinary action inputs and `run` scripts are not sent to
an input-only engine. Action inputs have a language, never an inferred runner shell, and cannot opt into ShellCheck
through a shell directive.

Engines replacing Ruff or ShellCheck receive the corresponding `tools.ruff` or `tools.shellcheck` settings for both
run scripts and declared inputs, including file overrides. ShellCheck's inline directives retain the input scalar's
source mapping; they do not add runner startup commands.

Go document analyzers requesting `analyzer.Scripts` receive these sites separately in `Input.ActionInputs`, with
`Site`, spans and optional composite `Invocation`. Their shell and runtime fields remain empty. `Input.Scripts`
and `Input.Invoked`, including the existing portable analyzer WIT, continue to contain only `run` scripts.

The internal tool contract supplies prepared snippets, source maps and execution
context to a registered runner. A runner returns findings with their originating
snippet index. Native process providers can use the same JSON request/result
protocol as core WASM: a `ScriptRequest` on stdin and an object containing `ok`,
`diagnostics` and `errors` on stdout. A nonzero process exit is an execution failure.
Native CLIs with their own protocols require an adapter; their argument construction,
configuration and diagnostic decoding belong to that adapter.

The engine and application accept tool registrations and do not import the bundled
Ruff/ShellCheck adapters or dispatch on their names. Process scheduling and WASM
loading remain separate from provider-specific preparation. The bundled adapters
preserve language-specific expression handling and source mapping.
