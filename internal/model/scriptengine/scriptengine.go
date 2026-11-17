// Package scriptengine defines the protocol between the host and the engines
// that check script snippets, independently of their transport: requests,
// diagnostics, engine descriptors and optional engine capabilities.
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

// Backend checks sanitized snippets. Check must honor cancellation,
// support concurrent calls, and finish all work before returning.
type Backend interface {
	Check(context.Context, Request) ([]Diagnostic, error)
}

// DescribedBackend exposes the selectors of a configured engine before
// script collection. Backends without this capability use native run routing.
type DescribedBackend interface {
	Backend
	ScriptDescriptor() Descriptor
}

// ConfigValidator is an optional backend capability. The engine validates
// its own configuration; the host does not maintain a second rule catalogue.
type ConfigValidator interface {
	ValidateConfig(context.Context, json.RawMessage) error
}

// TemplateValidator checks runtime support before template filtering.
// False with no error means an optional backend is unavailable.
type TemplateValidator interface {
	ValidateTemplates(context.Context) (bool, error)
}

// Descriptor describes a portable snippet engine independently of its
// transport. Rule names identify the host adapter it can replace.
//
// Fixes and ReportedSources declare what the engine's diagnostics may carry;
// hosts reject diagnostics that use an undeclared capability. Resources are
// the read-only grants the engine requests for each check; only
// checkinputs.ToolDirectory is defined for snippet engines.
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

// Clone isolates mutable descriptor payloads from callers.
func (d Descriptor) Clone() Descriptor {
	d.Shells = slices.Clone(d.Shells)
	d.ScriptInputs = slices.Clone(d.ScriptInputs)
	d.Resources = slices.Clone(d.Resources)
	d.Rule = d.Rule.Clone()
	return d
}

// Request is independent of process arguments and runtime memory layouts.
// Config contains only engine settings. Filename is a virtual snippet name and
// grants no filesystem access. Source has already had workflow expressions masked.
// Directory, when set, is the script's working directory; files an engine
// reports reading resolve from it. It grants no access by itself. Excluded
// lists diagnostic codes the preparation profile excludes, such as "SC1091";
// engines must not report them.
// Native suppression directives in Source belong to the engine. Return active
// findings after processing them; the host separately applies actionlint's YAML
// and configured ignores after admitting and mapping returned diagnostics.
//
// DirectoryFS is the host's read-only grant of Directory for this call, or
// nil. The host opens it inside the workspace and records the files read
// through it. A sandboxed transport gives its engine only this grant, never
// Directory as a path, and only when the engine requested the directory
// resource. The caller keeps it open until Check returns.
type Request struct {
	Source      string                     `json:"source"`
	Filename    string                     `json:"filename"`
	Config      json.RawMessage            `json:"config,omitempty"`
	Directory   string                     `json:"directory,omitempty"`
	Excluded    []string                   `json:"excluded,omitempty"`
	Environment []scripts.EnvironmentNames `json:"environment,omitempty"`
	DirectoryFS fs.FS                      `json:"-"`
}

// Diagnostic is a transport-independent snippet finding. Diagnostics may
// accompany an error when a backend completes only part of its analysis.
//
// Positions are one-based, count Unicode scalar values and use LF-delimited
// decoded-source lines; EndLocation is exclusive, and its zero value means
// the diagnostic has no end. Transports convert their engine's own line and
// column conventions before returning diagnostics.
//
// When a finding refers to another file read by the engine, Source gives its
// path relative to the request directory. Positions refer to that file.
// Fixes use snippet coordinates. Engines must declare the corresponding
// capabilities to return reported sources or fixes.
//
// The JSON form, which core JSON engines and native process engines return,
// spells positions as {"row", "column"}; see wire.go.
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

// Fix is one atomic alternative made of replacements in snippet coordinates.
type Fix struct {
	// Applicability defaults to review-required when an older engine omits it.
	Applicability applicability.Applicability
	Description   string
	Replacements  []Replacement
}

// Replacement replaces the text between two snippet positions. Positions
// follow Diagnostic; End is exclusive.
type Replacement struct {
	Start sourcetext.Position
	End   sourcetext.Position
	Text  string
}
