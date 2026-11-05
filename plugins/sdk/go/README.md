# Go plugin SDK

This module provides Go guest bindings for actionlint's component interfaces.
It contains no host implementation. Build a plugin for WebAssembly with TinyGo;
these bindings declare Wasm imports and exports.

The [insecure-download example](../../examples/insecure-download/main.go) assigns
its descriptor and check functions to `analyzer.Exports`:

```go
import "actionlint.kjanat.dev/v2/plugins/sdk/go/bindings/kjanat/actionlint/analyzer"

func init() {
    analyzer.Exports.Describe = describe
    analyzer.Exports.Analyze = analyze
}
```

`describe` returns `analyzer.Descriptor`. `analyze` accepts `analyzer.Input` and
returns `analyzer.Outcome`. The example implements both functions and maps script
positions back to the supplied source.

From the repository root, run `mise run protocol:go` after changing
`plugins/protocol/wit`. This regenerates the bindings with `wit-bindgen-go`.
