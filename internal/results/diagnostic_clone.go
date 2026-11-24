package results

//go:generate go run ../development/generators/deep-copies -root ../.. -package ./internal/results -type Diagnostic -output internal/results/deep_copy.gen.go

func (d Diagnostic) Clone() Diagnostic { return deepCopyDiagnostic(d) }
