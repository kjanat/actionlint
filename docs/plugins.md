# Plugins

actionlint runs WebAssembly components that implement the `kjanat:actionlint/analyzer@0.1.0` interface. The contract is
[`plugins/wit/analyzer.wit`](../plugins/wit/analyzer.wit). A plugin describes itself once, then receives one document per
call with the facts it declared and returns diagnostics. Components may instead implement the independent
`kjanat:actionlint/runner-provider@0.1.0` interface, or implement both roles. Any language with a component toolchain can implement them.

## Load a plugin

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

The host refuses a component at load when it exports an unsupported version of either recognized interface, imports an interface
the host does not grant, or exports neither role. Before guest startup it checks each implemented interface's complete
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
The pinned [`kjanat/wacogo`](https://github.com/kjanat/wacogo/tree/b54ae498a6fa22b3fb53c88e2aa34c54d7f21bd1)
runtime adds these allocation checks to wacogo.

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

The public Go package `actionlint.kjanat.dev/plugin` provides `plugin.Load`, `plugin.Options` and `plugin.Limits`.

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
https://github.com/partite-ai/wacogo/pull/12. The pinned runtime includes this fix.

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

From Go, load it with `plugin.Load` from `actionlint.kjanat.dev/plugin`, wrap it in `analyzer.Registration` and pass it
in `analysis.Options.Analyzers`. [`plugin/example_test.go`](../plugin/example_test.go) demonstrates the public API.
The internal integration tests also cover composite action calls. When parsing configuration yourself, pass the
registered analyzer names to `config.Parse`, `config.Load` or `config.ParseOverlay` so suppression names can be validated. File analysis and
the CLI derive those names from the registered analyzers automatically. The Rust crate under
`testdata/plugins/misbehave` shows how traps, panics, exits, allocation beyond the limit, hangs and denied
file access are reported.

## Script engines

Python and shell snippets go through a second contract, `analyzer.ScriptBackend`: actionlint selects the snippet,
masks workflow expressions and maps positions back, and the engine checks one sanitized snippet at a time.
[actionlint-plugin-ruff](https://github.com/kjanat/actionlint-plugin-ruff) implements it as a core WebAssembly module
with a JSON ABI: `check`, `validate_config` and `metadata` exports, no imports, requests and responses as UTF-8 JSON in
linear memory. Select such a module in the configuration:

```yaml
tools:
  ruff:
    backend:
      wasm:
        path: ./tools/actionlint-plugin-ruff.wasm.gz
```

The host checks the ABI version, the export signatures and the advertised operation versions when it loads the module,
refuses modules with imports, and runs each call in a pooled instance under a 256 MiB memory limit and a 10 second time
limit. Defined tables require finite maxima; conservative reservations for their maximum size and growth buffers share
that memory budget with linear memory. Components charge each core instance, including repeated and nested modules;
imported table aliases are charged to their defining instance only. Artifact acquisition defaults to 128 MiB for both raw and gzip-expanded bytes. Every response, including metadata
and configuration validation, must fit within 8 MiB before the host reads, copies or decodes it.
Initialization and creation of pooled instances use the same time limit. A trap or an exceeded limit, including
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

Core script engines use the version 1 JSON ABI to analyze sanitized snippets.
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
