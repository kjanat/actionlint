package results

//go:generate go run ../development/generators/owned-copies -root ../.. -package ./internal/results -type Diagnostic -output internal/results/owned_copy.gen.go

// Clone detaches all mutable diagnostic fields from the original.
func (d Diagnostic) Clone() Diagnostic { return copyOwnedDiagnostic(d) }
