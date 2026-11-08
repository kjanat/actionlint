# Scalar source maps

`Document` indexes YAML source lines and their original line endings. `Decode`
reads a scalar once and returns its decoded text, the source locations of its
characters, and the scalar's start and end positions. The containing collection
supplies the indentation and whether the scalar is inside a flow-style mapping
or sequence. The decoder determines where the scalar ends from YAML syntax,
rather than searching the source for a prefix matching the decoded text.

All scalar styles use the same code to record decoded characters and their
locations. Plain and quoted values share whitespace handling; literal and folded
blocks share indentation and trailing-newline handling. Each decoded escape maps
to the complete escape sequence in the source. Whitespace created by folding or
line-break normalization has no token position. Unicode line separators (LS) and
paragraph separators (PS) map across source lines.

The actionlint adapter compares the complete result with `syntax.Node.Value` before
using a map. Workflow expressions, metadata expressions, and external script
checkers use that map. Inline suppressions use the scalar extent from the same
scan, including closing quotes. Malformed-YAML recovery also uses this decoder to
find where blocks end.

`TestScalarDecoderInvariants` and `FuzzScalarDecoderInvariants` in
`internal/model/workflows/parser/scalar_decoder_invariant_test.go`
compare values with the YAML parser and check each copied/escaped source span,
bounds, line-break spans, and synthesized whitespace. The recorded runner corpus
is tested separately by `TestScalarSourceReferenceRecorded` in
`internal/development/tests/conformance/yaml/scalar_source_reference_test.go`.
