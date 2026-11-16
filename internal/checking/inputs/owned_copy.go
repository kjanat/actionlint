package inputs

//go:generate go run ../../development/generators/owned-copies -root ../../.. -package ./internal/checking/inputs -type Input,Descriptor -output internal/checking/inputs/owned_copy.gen.go

// Detached returns an input with its mutable data isolated from the caller.
// Immutable graph and interpreter handles and explicit capabilities stay shared.
func (input AnalyzerInput) Detached() AnalyzerInput { return copyOwnedInput(input) }

// Detached returns a descriptor with independently owned metadata.
func (d Descriptor) Detached() Descriptor { return copyOwnedDescriptor(d) }
