# Plugins

actionlint runs WebAssembly components that implement the `kjanat:actionlint/analyzer@0.1.0` interface. The contract is
[`plugins/wit/analyzer.wit`](../plugins/wit/analyzer.wit). A plugin describes itself once, then receives one document per
call with the facts it declared and returns diagnostics. Any language with a component toolchain can implement it.

## Load a plugin

```sh
actionlint --plugin ./pipe-to-shell.wasm --plugin ./no-sudo.wasm
```

Each `--plugin` loads one component. Findings carry the plugin name as their rule, so `--ignore-regex`, inline
suppressions and configured ignores apply to them like built-in rules. `--log-level debug` prints the plugin's limits
when it loads and forwards its log lines, stdout and stderr.

The host refuses a component at load when it exports another version of the analyzer interface, imports an interface
the host does not grant, or does not export the analyzer at all.

## Limits and capabilities

Every analysis call runs in a fresh instance with 64 MiB of memory and 10 seconds of time. A trap, a panic, an exit, an
exhausted limit or a cancelled run fails the analysis of that document with an error that names the plugin and its
limits. The run then continues with the next document and actionlint exits with status 3.

Component results and host-import arguments also have per-call budgets of 262,144 values and 8 MiB of accounted
string and collection storage. Nested values and repeated aliases count toward these budgets; validation runs
before host allocations and string encoding conversions.
The pinned [`kjanat/wacogo`](https://github.com/kjanat/wacogo/tree/b54ae498a6fa22b3fb53c88e2aa34c54d7f21bd1)
runtime adds these allocation checks to wacogo.

The host grants these imports and nothing else:

| Interface                        | What the plugin gets                                              |
| -------------------------------- | ----------------------------------------------------------------- |
| `kjanat:actionlint/host@0.1.0`   | `log`, one line into actionlint's debug log                       |
| `wasi:cli/*`, `wasi:io/*`        | stdout and stderr into the debug log, empty stdin and environment |
| `wasi:clocks/*`, `wasi:random/*` | clocks and random numbers                                         |
| `wasi:filesystem/*`              | no preopened directories, so every open fails                     |

`wasi:sockets`, `wasi:http` and custom interfaces are refused at load.

The public Go package `actionlint.kjanat.dev/plugin` provides `plugin.Load`, `plugin.Options` and `plugin.Limits`.

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
https://github.com/partite-ai/wacogo/pull/12. The pinned runtime includes this fix.

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

From Go, load it with `plugin.Load` from `actionlint.kjanat.dev/plugin`, wrap it in `analyzer.Registration` and pass it
in `analysis.Options.Analyzers`. [`plugin/example_test.go`](../plugin/example_test.go) demonstrates the public API.
The internal integration tests also cover composite action calls. The Rust crate under
`internal/component/testdata/misbehave` shows how traps, panics, exits, allocation beyond the limit, hangs and denied
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
limit. Raw and gzip-expanded modules must fit within 128 MiB before compilation. Every response, including metadata
and configuration validation, must fit within 8 MiB before the host reads, copies or decodes it.
A trap or an exceeded limit discards that instance and fails the document's analysis. From Go, call
`plugin.LoadEngine` and set the result as `analysis.Tool.Backend`; library tool settings take precedence over the
repository configuration.

## Contribute a rule

Plugins live in their own repositories and ship a component. Open an issue in this repository to list a plugin in the
documentation.

Built-in rules are Go code: visitor rules in `internal/engine` with catalog entries in `internal/repoconfig/catalog.go`
and documentation in [`docs/checks.md`](checks.md), or fact-based analyzers that implement `analyzer.Analyzer` as
described in the [Go API](api.md#add-an-analyzer). Changes to the WIT contract keep `kjanat:actionlint/analyzer@0.1.0`
usable and add a new version for incompatible changes.
