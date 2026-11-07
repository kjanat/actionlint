# Go plugin SDK

This module provides Go guest bindings for actionlint's component interfaces.
It contains no host implementation. Build a plugin for WebAssembly with TinyGo;
these bindings declare Wasm imports and exports.

The [insecure-download example](../../examples/insecure-download/main.go) assigns
its descriptor and check functions to `analyzer.Exports`:

```go
import "actionlint.kjanat.dev/v2/plugins/sdk/go/bindings/kjanat/actionlint/v0.2.0/analyzer"

func init() {
    analyzer.Exports.Describe = describe
    analyzer.Exports.Analyze = analyze
}
```

`describe` returns `analyzer.Descriptor`. `analyze` accepts `analyzer.Input` and
returns `analyzer.Outcome`. The example implements both functions and maps script
positions back to the supplied source.

## Package a component

Use `packaging.Package` from a native entry point excluded from the Wasm build:

```go
import "actionlint.kjanat.dev/v2/plugins/sdk/go/packaging"
```

Supply the plugin's release identity, engine, supported languages, capabilities,
license text and the descriptor returned by `describe()`. Call
`Package.Write(componentPath, outputDirectory)` to write `plugin.wasm`,
`metadata.json`, `schema.json`, `LICENSE` and `SHA256SUMS`. It replaces only the
outer component's discovery section; nested component and module bytes are
copied unchanged. Run `wasm-tools validate` on the result before publishing it.

The [example's native entry point](../../examples/insecure-download/main_native.go)
shows the complete call. Its [build script](../../examples/insecure-download/build.sh)
builds the guest, packages it and validates the result.

`WithMetadata` is the lower-level operation for an existing JSON metadata object.
It checks the component header and section lengths. It does not validate Wasm
instructions or replace registry validation.

## Read script environment names

Import `bindings/kjanat/actionlint-script/v0.3.0/environments` to query names for an
occurrence supplied to the current check. `ForScript` returns each OS
alternative separately. `Current` is for a snippet-engine check. The results
contain declaration scopes and name comparisons, without values or secrets.
They do not enumerate ambient runner variables or changes through `GITHUB_ENV`.

Include `kjanat:actionlint/environment-plugin@0.2.0` instead of
`kjanat:actionlint/plugin@0.2.0` in the component's WIT world when calling these
functions. Copy the protocol's `deps/` packages alongside the actionlint package
when composing WIT packages, as the example's build script does.

```go
import (
    "errors"
    "actionlint.kjanat.dev/v2/plugins/sdk/go/bindings/kjanat/actionlint-script/v0.3.0/environments"
)

func environmentsFor(id string) ([]environments.Environment, error) {
    result := environments.ForScript(id)
    if environments := result.OK(); environments != nil {
        return environments.Slice(), nil
    }
    if message := result.Err(); message != nil {
        return nil, errors.New(*message)
    }
    return nil, errors.New("environment query returned no result")
}
```

Compare names using each alternative's `LookupComparison`.
`MergeComparison` describes how the runner combines declarations.

## Update the bindings

From the repository root, run `mise run protocol:go` after changing
`plugins/protocol/wit`. This refreshes the SDK's contract source and regenerates
the bindings with `wit-bindgen-go`. Packaging derives the interface version and
contract checksum from that copied source.
