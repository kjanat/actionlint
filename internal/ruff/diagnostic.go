// Package ruff implements the JSON protocol for embedded Python analysis.
package ruff

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// Position uses Ruff's one-based Unicode character coordinates.
type Position struct {
	Row    int `json:"row"`
	Column int `json:"column"`
}

// Diagnostic is the read-only subset of Ruff's JSON diagnostic contract.
type Diagnostic struct {
	Filename    string   `json:"filename"`
	Code        string   `json:"code"`
	Name        string   `json:"name"`
	Message     string   `json:"message"`
	Location    Position `json:"location"`
	EndLocation Position `json:"end_location"`
}

// Decode validates the entire response before callers publish any findings.
func Decode(data []byte) ([]Diagnostic, error) {
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("[")) {
		return nil, errors.New("expected a JSON array from Ruff")
	}
	var diagnostics []Diagnostic
	if err := json.Unmarshal(data, &diagnostics); err != nil {
		return nil, fmt.Errorf("invalid Ruff JSON: %w", err)
	}
	for i := range diagnostics {
		d := &diagnostics[i]
		// Preview diagnostics such as invalid-syntax have a null code.
		if d.Code == "" {
			d.Code = d.Name
		}
		if d.Code == "" || d.Message == "" || d.Location.Row < 1 || d.Location.Column < 1 {
			return nil, fmt.Errorf("invalid Ruff diagnostic: %+v", d)
		}
	}
	return diagnostics, nil
}
