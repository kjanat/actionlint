// Package process decodes the output of native tool processes into script
// diagnostics. Decoders are pure: they read stdout and the argument variables
// of one check and never touch the filesystem, the environment or the network.
package process

import (
	"context"
	"fmt"

	"actionlint.kjanat.dev/v2/analyzer"
)

// Limits shared with the component script-engine host.
const (
	// MaxOutputBytes bounds a tool's stdout and a transform's output.
	MaxOutputBytes = 8 << 20
	// MaxDiagnostics bounds the diagnostics of one check.
	MaxDiagnostics = 10000
)

// Decoder turns the stdout of one check into diagnostics. Vars are the
// argument variables of the check, such as "filename"; Decode must honour
// ctx cancellation.
type Decoder interface {
	Decode(ctx context.Context, stdout []byte, vars map[string]string) (Result, error)
}

// Result is one decoded check. Errors are engine-reported failures; they may
// accompany diagnostics when the engine completed only part of its analysis.
// Sources are files the engine read besides the snippet. Directory is where
// the engine ran, from which relative sources resolve; decoders leave it
// empty and Backend sets it.
type Result struct {
	Diagnostics []analyzer.ScriptDiagnostic `json:"diagnostics"`
	Sources     []string                    `json:"sources,omitempty"`
	Errors      []string                    `json:"errors,omitempty"`
	Directory   string                      `json:"-"`
}

// checkInput rejects stdout larger than any decoder accepts.
func checkInput(stdout []byte) error {
	if len(stdout) > MaxOutputBytes {
		return fmt.Errorf("tool output exceeds %d bytes", MaxOutputBytes)
	}
	return nil
}

// checkCount rejects results with more diagnostics than the host accepts.
func checkCount(count int) error {
	if count > MaxDiagnostics {
		return fmt.Errorf("tool returned more than %d diagnostics", MaxDiagnostics)
	}
	return nil
}
