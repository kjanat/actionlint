package analyzer

import (
	"context"
	"encoding/json"
	"slices"

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
	Rule          rule.Descriptor
}

// Clone isolates mutable descriptor payloads from callers.
func (d ScriptEngineDescriptor) Clone() ScriptEngineDescriptor {
	d.Shells = slices.Clone(d.Shells)
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
