package checkinputs

//go:generate go run ../../development/generators/owned-copies -root ../../.. -package ./internal/model/checkinputs -type AnalyzerInput,Descriptor -output internal/model/checkinputs/owned_copy.gen.go

// Detached shares immutable graph and interpreter handles and explicit
// capabilities with the caller.
func (input AnalyzerInput) Detached() AnalyzerInput { return copyOwnedAnalyzerInput(input) }

func (d Descriptor) Detached() Descriptor { return copyOwnedDescriptor(d) }
