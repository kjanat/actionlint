package checkinputs

//go:generate go run ../../development/generators/owned-copies -root ../../.. -package ./internal/model/checkinputs -type AnalyzerInput,Descriptor -output internal/model/checkinputs/owned_copy.gen.go

// Detached returns an input with its mutable data isolated from the caller.
// Immutable graph and interpreter handles and explicit capabilities stay shared.
func (input AnalyzerInput) Detached() AnalyzerInput { return copyOwnedAnalyzerInput(input) }

// Detached returns a descriptor with independently owned metadata.
func (d Descriptor) Detached() Descriptor { return copyOwnedDescriptor(d) }
