package process

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
)

// Envelope decodes the {ok, diagnostics, errors} check result a core-WASM
// engine returns, written to stdout by a native engine.
type Envelope struct{}

// Decode yields one generic error for an engine that reports failure without a message.
func (Envelope) Decode(ctx context.Context, stdout []byte, _ map[string]string) (DecodedCheck, error) {
	if err := ctx.Err(); err != nil {
		return DecodedCheck{}, err
	}
	if err := checkInput(stdout); err != nil {
		return DecodedCheck{}, err
	}
	var envelope struct {
		OK          *bool                     `json:"ok"`
		Diagnostics []scriptengine.Diagnostic `json:"diagnostics"`
		Errors      []string                  `json:"errors"`
	}
	if err := jsonv2.Unmarshal(stdout, &envelope); err != nil {
		return DecodedCheck{}, fmt.Errorf("decode tool response: %w", err)
	}
	if envelope.OK == nil {
		return DecodedCheck{}, errors.New("tool response is missing ok")
	}
	if err := checkCount(len(envelope.Diagnostics)); err != nil {
		return DecodedCheck{}, err
	}
	result := DecodedCheck{Diagnostics: envelope.Diagnostics, Errors: envelope.Errors}
	if !*envelope.OK && len(result.Errors) == 0 {
		result.Errors = []string{"tool reported failure without an explanation"}
	}
	return result, nil
}
