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
// Sources are files the engine read besides the snippet.
type Result struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
	Sources     []string     `json:"sources,omitempty"`
	Errors      []string     `json:"errors,omitempty"`
}

// Diagnostic is an analyzer.ScriptDiagnostic plus the reported source file
// and suggested fixes, which the snippet contract does not carry yet.
type Diagnostic struct {
	analyzer.ScriptDiagnostic
	// Source is a file the engine read, relative to its working directory,
	// when the finding is not in the snippet.
	Source string `json:"source,omitempty"`
	Fixes  []Fix  `json:"fixes,omitempty"`
}

// Fix is one suggested edit made of replacements in script coordinates.
type Fix struct {
	Description  string        `json:"description,omitempty"`
	Replacements []Replacement `json:"replacements"`
}

// Replacement replaces the text between two script positions.
type Replacement struct {
	Row       int    `json:"row"`
	EndRow    int    `json:"end_row"`
	Column    int    `json:"column"`
	EndColumn int    `json:"end_column"`
	Text      string `json:"text"`
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
