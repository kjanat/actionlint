# Scalar source mapping reference

`reference.json` records 18 requests and responses from the original runner's
next YAML/template parser at runner commit
`67f01c276e0a91d967ba499ce4cdc9a95efa0262`.
Only elapsed time (`ns`) and allocation counts (`alloc`) are omitted.

The ordinary `TestScalarSourceReferenceRecorded` test compares decoded scalar
values and YAML start positions with these results and requires a source mapping.
The reference reports scalar positions. Separate raw-YAML tests verify token
locations and exclusive end positions. Unknown expression names intentionally
produce reference template errors alongside the parsed YAML result.

Run the same inputs through a locally available Runny Docker image:

```sh
ACTIONLINT_REFERENCE_IMAGE=ghcr.io/kjanat/runny-mc-fuzzface:latest \
go test ./internal/workflow -run '^TestScalarSourceReference' -count=1 -v
```

The live test checks the complete Runny reports, including runner commit, decoded
values, source positions and template errors. It validates the recorded request IDs
and protocol version separately; those envelope fields are absent from Docker's
one-shot reports. Only timing and allocation measurements are excluded from report
comparison. The test never pulls or builds an image. An unset image skips live
verification; an unavailable or incompatible image fails it.
