# Plugins

actionlint runs WebAssembly components that implement the `kjanat:actionlint/analyzer@0.1.0` interface. The contract is
[`plugins/wit/analyzer.wit`](../plugins/wit/analyzer.wit). A plugin describes itself once, then receives one document per
call with the facts it declared and returns diagnostics. Any language with a component toolchain can implement it.

## Load a plugin

```sh
actionlint --plugin ./pipe-to-shell.wasm --plugin ./no-sudo.wasm
```

Each `--plugin` loads one component or a supported core script engine. Findings carry the plugin name as their rule, so `--ignore-regex`, inline
suppressions and configured ignores apply to them like built-in rules. `--log-level debug` prints the plugin's limits
when it loads and forwards its log lines, stdout and stderr.

The host refuses a component at load when it exports another version of the analyzer interface, imports an interface
the host does not grant, or does not export the analyzer at all.

## Limits and capabilities

Every analysis call runs in a fresh instance with 64 MiB of memory and 10 seconds of time. A trap, a panic, an exit, an
exhausted limit or a cancelled run fails the analysis of that document with an error that names the plugin and its
limits. The run then continues with the next document and actionlint exits with status 3.

Stdout and stderr share a 1 MiB output budget per call, including when debug logging is disabled. Exceeding it fails
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

## Write a plugin

The world is `plugin`: it imports `host` and exports `analyzer`. `describe` returns the plugin name and the facts it
needs. `analyze` receives the document path and content, declared scripts, scripts invoked through composite actions,
mapping names and the JSON options the host registered, and returns a list of diagnostics or an error message.

Script text comes with spans that map each line and column back to the declaring document. The WIT comment on `span`
gives the lookup. A diagnostic with an empty `rule` takes the plugin name, and an empty `path` means the analyzed
document, so findings in a composite action copy `script.path`.

### Rust

[`plugins/examples/pipe-to-shell`](../plugins/examples/pipe-to-shell) flags `curl ... | sh` patterns. It depends on
the `wit-bindgen` crate only and builds with:

```sh
rustup target add wasm32-wasip2
cargo build --release --target wasm32-wasip2
```

The component is `target/wasm32-wasip2/release/actionlint_plugin_pipe_to_shell.wasm`.

### C

[`plugins/examples/no-sudo`](../plugins/examples/no-sudo) flags `sudo` at the start of a command. `build.sh` generates
bindings with `wit-bindgen c` and compiles with wasi-sdk:

```sh
WASI_SDK_PATH=/opt/wasi-sdk ./build.sh no-sudo.wasm
```

### TinyGo

[`plugins/examples/insecure-download`](../plugins/examples/insecure-download) flags `curl -k` and
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

### Build outside this repository

Copy `plugins/wit` next to your sources and point the binding generator at it. The examples refer to `../../wit`, so
the layout `wit/` beside `examples/<plugin>/` reproduces their build as is. No other file from this repository is
needed.

## Test a plugin

Run it against a workflow:

```sh
actionlint --no-config --plugin ./plugin.wasm .github/workflows/ci.yml
```

From Go, load it with `plugin.Load` and pass it in `analysis.Options.Analyzers`; `internal/plugins/component/component_test.go`
does this for the examples, including a composite action call. When parsing configuration yourself, pass the
registered analyzer names to `config.Parse` or `config.Load` so suppression names can be validated. File analysis and
the CLI derive those names from the registered analyzers automatically. The Rust crate under
`internal/plugins/component/testdata/misbehave` shows how traps, panics, exits, allocation beyond the limit, hangs and denied
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

Core script engines use the version 1 JSON ABI because they analyze sanitized
snippets, not workflow documents.
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
