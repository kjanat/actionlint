# Scalar source maps

`Document` indexes physical YAML lines and their original line endings. `Decode`
scans a scalar once, emitting its decoded value, source spans, and lexical extent
together. The containing YAML collection supplies indentation and flow context.
No decoded-value prefix matching determines where a scalar ends.

All styles use the same emitter. Plain and quoted values share flow-whitespace
handling; literal and folded blocks share indentation and chomping. Escapes map
to the complete source escape. Folding and normalized line breaks emit whitespace
without a token position. Preserved LS/PS line breaks have cross-line spans.

The actionlint adapter compares the complete result with `yaml.Node.Value` before
using a map. Workflow expressions, metadata expressions, and external script
checkers use that map. Inline suppressions use the scalar extent from the same
scan, including closing quotes. Malformed-YAML recovery also uses this decoder to
find block extents, without claiming a fully validated YAML value.

`TestScalarDecoderInvariants` and `FuzzScalarDecoderInvariants` in the root package
compare values with the YAML parser and check each copied/escaped source span,
bounds, line-break spans, and synthesized whitespace. The recorded runner corpus
is tested separately by `TestScalarSourceReferenceRecorded`.
