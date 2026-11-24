# Component contract generation

`run contract` reads the WIT interfaces in `plugins/protocol/wit` and generates the types and enum order for the loader and the Go adapters. The loader checks each plugin's interface types against them before instantiation.

From this directory, `mise exec -- go run . -fixtures` also regenerates the compatibility fixtures with `wasm-tools`. Each fixture changes one function signature, nested record field or enum order, and one control fixture changes nothing.

`TestCanonicalContractIsCurrent` fails when the generated files are stale.
