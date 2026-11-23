# Go plugin SDK

This module provides Go guest bindings for actionlint's component interfaces. The [insecure-download example](../../examples/insecure-download/main.go) assigns its functions to `analyzer.Exports`:

```go
import "actionlint.kjanat.dev/v2/plugins/sdk/go/bindings/kjanat/actionlint/v0.2.0/analyzer"

func init() {
    analyzer.Exports.Describe = describe
    analyzer.Exports.Analyze = analyze
}
```

## Package a component

`Package.Write(componentPath, outputDirectory)` writes `plugin.wasm`, `metadata.json`, `schema.json`, `LICENSE` and `SHA256SUMS`. Run `wasm-tools validate` on the result before you publish it. See the [example's native entry point](../../examples/insecure-download/main_native.go).

## Read script environment names

The `environments` bindings query names for an occurrence in the current check. `ForScript` returns each OS alternative separately.

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

Compare names with each alternative's `LookupComparison`.

## Update the bindings

Run `mise run protocol:go` from the repository root after you change `plugins/protocol/wit`.
