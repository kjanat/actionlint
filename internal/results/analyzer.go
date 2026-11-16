package results

import (
	"context"
	"encoding/json"
	"reflect"

	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
)

// Analyzer is a check that turns shared check inputs into diagnostics. It
// completes all work before returning. The host may call AnalyzeDocument
// concurrently for different documents. Implementations must honor cancellation.
// Findings may accompany an error when an analysis is incomplete.
// Descriptor and AnalyzeDocument must not panic; the host does not recover analyzer panics.
type Analyzer interface {
	Descriptor() checkinputs.Descriptor
	AnalyzeDocument(context.Context, checkinputs.AnalyzerInput) ([]Diagnostic, error)
}

// AnalyzerRegistration supplies analyzer-specific configuration without
// exposing the host's mutable configuration objects or rule registry.
type AnalyzerRegistration struct {
	Analyzer Analyzer
	Options  json.RawMessage
	// Prepare acquires additional facts before checking. On failure it returns
	// the usable partial input, so completed observations can still yield findings.
	Prepare func(context.Context, checkinputs.AnalyzerInput) (checkinputs.AnalyzerInput, error)
}

// IsNilAnalyzer detects both nil interfaces and typed nil analyzers before
// metadata or execution methods are called.
func IsNilAnalyzer(value Analyzer) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	if !nillableKinds[reflected.Kind()] {
		return false
	}
	return reflected.IsNil()
}

// nillableKinds lists the kinds whose values reflect.Value.IsNil accepts.
var nillableKinds = map[reflect.Kind]bool{
	reflect.Chan:      true,
	reflect.Func:      true,
	reflect.Interface: true,
	reflect.Map:       true,
	reflect.Pointer:   true,
	reflect.Slice:     true,
}
