# Scalar source maps

Each decoded escape maps to its whole escape sequence. Whitespace from folding and line-break normalization has no source position.

`TestScalarDecoderInvariants` and `FuzzScalarDecoderInvariants` in `internal/model/workflows/parser/scalar_decoder_invariant_test.go` check every span against the YAML parser.
