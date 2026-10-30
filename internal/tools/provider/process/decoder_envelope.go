package process

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/analyzer"
)

// Envelope decodes the {ok, diagnostics, errors} check result a core-WASM
// engine returns, written to stdout by a native engine.
type Envelope struct{}

// Decode implements Decoder. An engine reporting failure without a message
// yields one generic error.
func (Envelope) Decode(ctx context.Context, stdout []byte, _ map[string]string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := checkInput(stdout); err != nil {
		return Result{}, err
	}
	var envelope struct {
		OK          *bool                       `json:"ok"`
		Diagnostics []analyzer.ScriptDiagnostic `json:"diagnostics"`
		Errors      []string                    `json:"errors"`
	}
	if err := jsonv2.Unmarshal(stdout, &envelope); err != nil {
		return Result{}, fmt.Errorf("decode tool response: %w", err)
	}
	if envelope.OK == nil {
		return Result{}, errors.New("tool response is missing ok")
	}
	if err := checkCount(len(envelope.Diagnostics)); err != nil {
		return Result{}, err
	}
	result := Result{Diagnostics: envelope.Diagnostics, Errors: envelope.Errors}
	if !*envelope.OK && len(result.Errors) == 0 {
		result.Errors = []string{"tool reported failure without an explanation"}
	}
	return result, nil
}
