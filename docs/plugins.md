# Plugins

actionlint runs WebAssembly components that implement the `kjanat:actionlint/analyzer@0.1.0` interface. The contract is
[`plugins/wit/analyzer.wit`](../plugins/wit/analyzer.wit). A plugin describes itself once, then receives one document per
call with the facts it declared and returns diagnostics. Components may instead implement the independent
`kjanat:actionlint/runner-provider@0.1.0` interface or the
[`kjanat:actionlint/script-engine@0.1.0`](../plugins/wit/script-engine.wit) interface. Components can implement multiple
roles. Any language with a component toolchain can implement them.

## Install from the registry

Search the [published catalogue](https://plugins.actionlint.kjanat.dev/) using canonical repository names:

```sh
actionlint plugin search shellcheck
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

`owner/plugin` is shorthand for `github.com/owner/plugin`. A new dependency without `@version` selects `latest`.
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
the shared user cache, useful after cloning or in CI. Checks also restore missing pinned artifacts; populated
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

The language server retains loaded plugins until shutdown. Explicit `--plugin` selections and remote engine URLs
use a fixed snapshot for that session; restart to refresh them. Configured local engine files reload when their
content changes, and selecting a different engine URI loads that identity separately. Earlier engines remain
available to active analyses until shutdown. Every analysis still receives fresh document buffers and invocation state.

The host refuses a component at load when it exports an unsupported version of a recognized interface, imports an interface
the host does not grant, or exports no supported role. Before guest startup it checks each implemented interface's complete
type graph against the canonical WIT, including function signatures, record fields and enum ordering. The expected
graph and enum discriminants are generated with `go generate ./internal/plugins/component`; a test detects stale output.

## Limits and capabilities

Every analysis call runs in a fresh instance with 64 MiB of memory and 10 seconds of time, including waiting for
another invocation to finish. Queue admission honors cancellation. A trap, a panic, an exit, an
exhausted limit or a cancelled run fails the analysis of that document with an error that names the plugin and its
limits. The run then continues with the next document and actionlint exits with status 3.

Stdout, stderr and the direct `host.log` capability share a 1 MiB output budget per call, including when debug logging is disabled. Exceeding it fails
the analysis even if the plugin handles the write error. Log lines are emitted in chunks of at most 64 KiB, and an
unfinished line is flushed at the end of its call.

The host grants these imports and nothing else:

| Interface                        | What the plugin gets                                              |
| -------------------------------- | ----------------------------------------------------------------- |
| `kjanat:actionlint/host@0.1.0`   | `log`, one line into actionlint's debug log                       |
| `wasi:cli/*`, `wasi:io/*`        | stdout and stderr into the debug log, empty stdin and environment |
| `wasi:clocks/*`, `wasi:random/*` | clocks and random numbers                                         |
| `wasi:filesystem/*`              | no preopened directories, so every open fails                     |

`wasi:sockets`, `wasi:http` and custom interfaces are refused at load.

The public `plugin` package exposes the loader and its resource limits through `Options` and `Limits`.

## Runner providers

[`plugins/wit/runner.wit`](../plugins/wit/runner.wit) defines the separate `runner-plugin` world. A provider-only
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
Explicit shells and container defaults retain precedence. Providers contribute knowledge, not diagnostic policy.

### Offline Buildkite example

[`examples/plugins/buildkite-runners.rs`](../examples/plugins/buildkite-runners.rs) resolves an explicit queue mapping
snapshot. It is an offline example, not a live Agent API client or a claim of GitHub image/toolchain parity.
Each queue declares its name, labels, optional group, OS, architecture and default shell. All labels and the requested
group must match the same queue. The catalogue's completeness refers to the selected scope.

```sh
cargo build --locked --profile wasm --target wasm32-wasip2 --example buildkite-runners
actionlint --plugin target/wasm32-wasip2/wasm/examples/buildkite_runners.wasm \
  --runner-target buildkite --runner-provider buildkite-runners \
  --runner-scope my-org/my-repo --runner-snapshot runners.json workflow.yml
```

`runners.json` is a JSON envelope, with provider-specific catalogue JSON encoded in `content`:

```json
{
  "scope": "my-org/my-repo",
  "obtained_at": "2026-10-19T00:00:00Z",
  "freshness": "fresh",
  "complete": true,
  "content": "{\"queues\":[{\"name\":\"windows-build\",\"labels\":[\"buildkite-windows\",\"x64\"],\"group\":\"build\",\"os\":\"windows\",\"architecture\":\"x64\",\"shell\":\"pwsh\"}]}"
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

[`tools/runner-host`](../tools/runner-host) is a finite Wasmtime parity probe, not a full Rust actionlint port.
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
gives the lookup. A diagnostic with an empty `rule` takes the plugin name, and an empty `path` means the analyzed
document, so findings in a composite action copy `script.path`.

### Rust

[`examples/plugins/pipe-to-shell.rs`](../examples/plugins/pipe-to-shell.rs) flags `curl ... | sh` patterns. It depends on
the `wit-bindgen` crate only and builds with:

```sh
cargo build --locked --profile wasm --target wasm32-wasip2 --example pipe-to-shell
```

The component is `target/wasm32-wasip2/wasm/examples/pipe_to_shell.wasm` at the repository root.

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
https://github.com/partite-ai/wacogo/pull/12, and actionlint's `go.mod` requires that version.

### Other toolchains

Any toolchain that emits a component for the `plugin` world works.

### Build the examples

Run `make plugin-examples` from the repository root to build the Rust, C and TinyGo examples.
The Rust components are Cargo `[[example]]` targets. The C and TinyGo builds use their language toolchains.
Tests load the components through the plugin interface.

For an independent plugin, create a package with its own manifest and lockfile and vendor the public
[`plugins/wit`](../plugins/wit) contract. Point its binding generator at that copy. The host's fixture
workspace is not a dependency of external plugins.

## Test a plugin

Run it against a workflow:

```sh
actionlint --no-config --plugin ./plugin.wasm .github/workflows/ci.yml
```

From Go, load it with `plugin.Load` and pass it in `analysis.Options.Analyzers`; `internal/plugins/component/component_test.go`
does this for the examples, including a composite action call. When parsing configuration yourself, pass the
registered analyzer names to `config.Parse` or `config.Load` so suppression names can be validated. File analysis and
the CLI derive those names from the registered analyzers automatically. The Rust crate under
`testdata/plugins/misbehave` shows how traps, panics, exits, allocation beyond the limit, hangs and denied
file access are reported.

## Script engines

Python and shell snippets use `analyzer.ScriptBackend`: actionlint selects the snippet,
masks workflow expressions and maps positions back, and the engine checks one sanitized snippet at a time.
The preferred component interface is [`script-engine.wit`](../plugins/wit/script-engine.wit), world
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
limit. Initialization and creation of pooled instances use the same time limit. A trap or an exceeded limit, including
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

Script engines analyze sanitized snippets through either the typed component interface or the legacy version 1 JSON ABI.
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
`lint.rules.plugins`; semantic categories such as security remain metadata rather
than configuration namespaces for third-party rules:

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
