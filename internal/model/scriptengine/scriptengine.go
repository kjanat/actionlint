// Package scriptengine defines the transport-independent protocol between the host and script snippet engines.
package scriptengine

import (
	"context"
	"encoding/json"
	"io/fs"
	"slices"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/applicability"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/scripts"
)

type Backend interface {
	Check(context.Context, Request) ([]Diagnostic, error)
}

type DescribedBackend interface {
	Backend
	ScriptDescriptor() Descriptor
}

type ConfigValidator interface {
	ValidateConfig(context.Context, json.RawMessage) error
}

// TemplateValidator returns false with no error when an optional backend is unavailable.
type TemplateValidator interface {
	ValidateTemplates(context.Context) (bool, error)
}

type Descriptor struct {
	ID               string
	Version          string
	Name             string
	Engine           string
	EngineVersion    string
	TemplateContract string
	Shells           []string
	ScriptInputs     []checkinputs.ScriptInput
	Rule             rule.Descriptor
	Fixes            bool
	ReportedSources  bool
	Resources        []checkinputs.ToolResourceKind
}

func (d Descriptor) Clone() Descriptor {
	d.Shells = slices.Clone(d.Shells)
	d.ScriptInputs = slices.Clone(d.ScriptInputs)
	d.Resources = slices.Clone(d.Resources)
	d.Rule = d.Rule.Clone()
	return d
}

type Request struct {
	Source      string                     `json:"source"`
	Filename    string                     `json:"filename"`
	Config      json.RawMessage            `json:"config,omitempty"`
	Directory   string                     `json:"directory,omitempty"`
	Excluded    []string                   `json:"excluded,omitempty"`
	Environment []scripts.EnvironmentNames `json:"environment,omitempty"`
	DirectoryFS fs.FS                      `json:"-"`
}

// Diagnostic positions are one-based, count Unicode scalar values and use LF-delimited lines.
// EndLocation is exclusive, and its zero value means the diagnostic has no end.
type Diagnostic struct {
	Filename       string
	Code           string
	Name           string
	Message        string
	Severity       string
	Location       sourcetext.Position
	EndLocation    sourcetext.Position
	URL            string
	TemplateEffect TemplateEffect
	Source         string
	Fixes          []Fix
}

type Fix struct {
	Applicability applicability.Applicability
	Description   string
	Replacements  []Replacement
}

type Replacement struct {
	Start sourcetext.Position
	End   sourcetext.Position
	Text  string
}
