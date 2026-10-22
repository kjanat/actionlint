package analyzer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/rule"
)

// ScriptBackend checks a sanitized snippet. The adapter owns its execution
// mechanism: a process, an in-process engine, or a WebAssembly instance.
// It must finish its work before returning, honor cancellation, and support
// concurrent calls. A serialized WASM instance can be guarded or pooled by its
// adapter. The host retains YAML mapping, suppression, and scheduling.
type ScriptBackend interface {
	Check(context.Context, ScriptRequest) ([]ScriptDiagnostic, error)
}

// DescribedScriptBackend exposes the selectors of a configured engine before
// script collection. Backends without this capability retain native run routing.
type DescribedScriptBackend interface {
	ScriptBackend
	ScriptDescriptor() ScriptEngineDescriptor
}

// ScriptConfigValidator is an optional backend capability. The engine validates
// its own configuration; the host does not maintain a second rule catalogue.
type ScriptConfigValidator interface {
	ValidateConfig(context.Context, json.RawMessage) error
}

// ScriptEngineDescriptor describes a portable snippet engine independently of
// its transport. Rule names identify the host adapter it can replace.
type ScriptEngineDescriptor struct {
	ID            string
	Version       string
	Name          string
	Engine        string
	EngineVersion string
	Shells        []string
	ScriptInputs  []ScriptInput
	Rule          rule.Descriptor
}

// ScriptInput identifies source code supplied to one action input, independently
// of a runner shell. Action is a lowercase owner/repository and optional
// case-sensitive subpath, without a ref.
type ScriptInput struct {
	Action   string `json:"action"`
	Input    string `json:"input"`
	Language string `json:"language"`
}

// ValidateScriptInputs rejects ambiguous or noncanonical routing declarations.
func ValidateScriptInputs(inputs []ScriptInput) error {
	if len(inputs) > 256 {
		return errors.New("script input selectors exceed 256 entries")
	}
	seen := make(map[[2]string]bool, len(inputs))
	for _, input := range inputs {
		if err := ValidateScriptInput(input); err != nil {
			return err
		}
		key := [2]string{input.Action, input.Input}
		if seen[key] {
			return fmt.Errorf("invalid or duplicate script input selector %+v", input)
		}
		seen[key] = true
	}
	return nil
}

// ValidateScriptInput checks one routing declaration without imposing an engine's catalogue limit.
func ValidateScriptInput(input ScriptInput) error {
	if len(input.Action) > 4096 || len(input.Input) > 128 || len(input.Language) > 64 {
		return errors.New("script input selector exceeds action, input or language length limits")
	}
	parts := strings.Split(input.Action, "/")
	valid := len(parts) >= 2 && selectorWord(input.Input, false) && selectorWord(input.Language, false)
	for index, part := range parts {
		valid = valid && selectorWord(part, index >= 2) && part != "." && part != ".."
	}
	if !valid {
		return fmt.Errorf("invalid script input selector %+v", input)
	}
	return nil
}

func selectorWord(word string, uppercase bool) bool {
	if word == "" {
		return false
	}
	for _, ch := range word {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9', ch == '-', ch == '_', ch == '.':
		case uppercase && ch >= 'A' && ch <= 'Z':
		default:
			return false
		}
	}
	return true
}

// Clone isolates mutable descriptor payloads from callers.
func (d ScriptEngineDescriptor) Clone() ScriptEngineDescriptor {
	d.Shells = slices.Clone(d.Shells)
	d.ScriptInputs = slices.Clone(d.ScriptInputs)
	d.Rule = d.Rule.Clone()
	return d
}

// ScriptRequest is independent of process arguments and runtime memory layouts.
// Config contains only engine settings. Filename is a virtual snippet name and
// grants no filesystem access. Source has already had workflow expressions masked.
type ScriptRequest struct {
	Source   string          `json:"source"`
	Filename string          `json:"filename"`
	Config   json.RawMessage `json:"config,omitempty"`
}

// ScriptPosition is one-based and counts Unicode scalar values. End positions
// are exclusive. A transport adapter must convert other coordinate conventions.
type ScriptPosition struct {
	Row    int `json:"row"`
	Column int `json:"column"`
}

// ScriptDiagnostic uses the Ruff plugin's check-v1 data shape. Diagnostics may
// accompany an error when a backend completes only part of its analysis.
type ScriptDiagnostic struct {
	Filename    string         `json:"filename"`
	Code        string         `json:"code"`
	Name        string         `json:"name,omitempty"`
	Message     string         `json:"message"`
	Severity    string         `json:"severity,omitempty"`
	Location    ScriptPosition `json:"location"`
	EndLocation ScriptPosition `json:"end_location"`
	URL         string         `json:"url,omitempty"`
}
