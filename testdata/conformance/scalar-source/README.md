# Scalar source mapping reference

`reference.json` records 16 requests and responses from the original runner's
next YAML/template parser, compiled from runner commit
`67f01c276e0a91d967ba499ce4cdc9a95efa0262` into a workflow-capable WASI module.
Only elapsed time (`ns`) and allocation counts (`alloc`) are omitted.

The ordinary `TestScalarSourceReferenceRecorded` test compares decoded scalar
values and YAML start positions with these results and requires a source mapping.
The reference reports scalar positions. Separate raw-YAML tests verify token
locations and exclusive end positions. Unknown expression names intentionally
produce reference template errors alongside the parsed YAML result.

Run the same requests against a prebuilt Go/WASI host and workflow module:

```sh
ACTIONLINT_REFERENCE_ORACLE=/path/to/wasi-oracle \
ACTIONLINT_REFERENCE_WASM=/path/to/workflow.wasm \
GOMAXPROCS=2 mise exec -- go test -p 1 . -run '^TestScalarSourceReference' -v
```

The live test checks the complete recorded responses, including runner commit,
decoded values, source positions and template errors. It does not fetch or build
the reference parser. Supplying neither path skips the live test; supplying only
one is an error.
