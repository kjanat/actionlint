// Package process decodes the output of native tool processes into script
// diagnostics. Decoders are pure.
package process

import (
	"context"
	"fmt"

	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
)

// Limits shared with the component script-engine host.
const (
	MaxOutputBytes = 8 << 20
	MaxDiagnostics = 10000
)

// Decoder implementations must honour ctx cancellation.
type Decoder interface {
	Decode(ctx context.Context, stdout []byte, vars map[string]string) (DecodedCheck, error)
}

type DecodedCheck struct {
	Diagnostics []scriptengine.Diagnostic `json:"diagnostics"`
	Sources     []string                  `json:"sources,omitempty"`
	// Errors may accompany diagnostics when the engine completed only part of its analysis.
	Errors []string `json:"errors,omitempty"`
	// Decoders leave Directory empty, and Backend sets it.
	Directory string `json:"-"`
	// Skipped is set by the host when an optional process could not run.
	Skipped bool `json:"-"`
}

func checkInput(stdout []byte) error {
	if len(stdout) > MaxOutputBytes {
		return fmt.Errorf("tool output exceeds %d bytes", MaxOutputBytes)
	}
	return nil
}

func checkCount(count int) error {
	if count > MaxDiagnostics {
		return fmt.Errorf("tool returned more than %d diagnostics", MaxDiagnostics)
	}
	return nil
}
