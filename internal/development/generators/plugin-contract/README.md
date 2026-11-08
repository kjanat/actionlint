# Component contract generation

`go generate ./internal/plugins/execution/component` reads the interfaces in `plugins/protocol/wit` and generates the
types and enum ordering that the loader expects. Go adapters translate internal models using that enum order.
Before creating a plugin instance, the loader checks its complete interface types against the generated definitions.
Loading a plugin does not run a WIT compiler.

From this directory, `mise exec -- go run . -fixtures` also regenerates the structural compatibility fixtures using
the `wasm-tools` version configured in [`.mise.toml`](../../../../.mise.toml).
These fixtures use the expected interface name but change a function signature, a nested
record field or enum order. A compatible control has no such changes. The fixture functions trap if called, so the
tests can distinguish rejection during type validation from a failure when calling `describe`.

The generated source includes a digest of the WIT with normalized line endings. `TestCanonicalContractIsCurrent`
fails if the canonical source changes without regeneration; compatibility tests exercise the compiled fixtures.
