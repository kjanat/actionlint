package results

import (
	"context"
	"encoding/json"
	"reflect"

	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
)

// Analyzer implementations must honor cancellation and must not panic, and the host may call AnalyzeDocument concurrently.
type Analyzer interface {
	Descriptor() checkinputs.Descriptor
	AnalyzeDocument(context.Context, checkinputs.AnalyzerInput) ([]Diagnostic, error)
}

type AnalyzerRegistration struct {
	Analyzer Analyzer
	Options  json.RawMessage
	// On failure Prepare returns the usable partial input.
	Prepare func(context.Context, checkinputs.AnalyzerInput) (checkinputs.AnalyzerInput, error)
}

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

var nillableKinds = map[reflect.Kind]bool{
	reflect.Chan:      true,
	reflect.Func:      true,
	reflect.Interface: true,
	reflect.Map:       true,
	reflect.Pointer:   true,
	reflect.Slice:     true,
}
