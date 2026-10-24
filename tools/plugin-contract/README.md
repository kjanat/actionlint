# Component contract generation

`go generate ./internal/plugins/component` parses the contracts in `crates/actionlint-extension-api/wit` and generates the complete expected
interface graph plus enum names in WIT order. Explicit Go adapters still translate internal models; they use the
generated enum order. The loader checks the graph before instantiating guest code. No WIT compiler runs at runtime.

From this directory, `go run . -fixtures` also regenerates the structural compatibility fixtures using `wasm-tools`
(generated with 1.262.0). These contain the same interface name as the canonical contract. One changes a function
signature, one a nested record field, and one enum order; the compatible control changes nothing. Their dummy
functions trap, proving rejection occurs before calling `describe`.

The generated source includes a digest of the WIT with normalized line endings. `TestCanonicalContractIsCurrent`
fails if the canonical source changes without regeneration; compatibility tests exercise the compiled fixtures.
