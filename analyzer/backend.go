package analyzer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/rule"
)

// ScriptBackend checks sanitized snippets. Check must honor cancellation,
// support concurrent calls, and finish all work before returning.
type ScriptBackend interface {
	Check(context.Context, ScriptRequest) ([]ScriptDiagnostic, error)
}

// DescribedScriptBackend exposes the selectors of a configured engine before
// script collection. Backends without this capability use native run routing.
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
//
// Fixes and ReportedSources declare what the engine's diagnostics may carry;
// hosts reject diagnostics that use an undeclared capability. Resources are
// the read-only grants the engine requests for each check; only
// ToolDirectory is defined for snippet engines.
type ScriptEngineDescriptor struct {
	ID               string
	Version          string
	Name             string
	Engine           string
	EngineVersion    string
	TemplateContract string
	Shells           []string
	ScriptInputs     []ScriptInput
	Rule             rule.Descriptor
	Fixes            bool
	ReportedSources  bool
	Resources        []ToolResourceKind
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
	d.Resources = slices.Clone(d.Resources)
	d.Rule = d.Rule.Clone()
	return d
}

// ScriptRequest is independent of process arguments and runtime memory layouts.
// Config contains only engine settings. Filename is a virtual snippet name and
// grants no filesystem access. Source has already had workflow expressions masked.
// Directory, when set, is the script's working directory; files an engine
// reports reading resolve from it. It grants no access by itself. Excluded
// lists diagnostic codes the preparation profile excludes, such as "SC1091";
// engines must not report them.
//
// DirectoryFS is the host's read-only grant of Directory for this call, or
// nil. The host opens it inside the workspace and records the files read
// through it. A sandboxed transport gives its engine only this grant, never
// Directory as a path, and only when the engine requested the directory
// resource. The caller keeps it open until Check returns.
type ScriptRequest struct {
	Source      string          `json:"source"`
	Filename    string          `json:"filename"`
	Config      json.RawMessage `json:"config,omitempty"`
	Directory   string          `json:"directory,omitempty"`
	Excluded    []string        `json:"excluded,omitempty"`
	DirectoryFS fs.FS           `json:"-"`
}

// ScriptTemplateValidator checks runtime support before template filtering.
// False with no error means an optional backend is unavailable.
type ScriptTemplateValidator interface {
	ValidateTemplates(context.Context) (bool, error)
}

// ScriptPosition is one-based, counts Unicode scalar values and uses LF-delimited
// decoded-source rows. End positions are exclusive. Transport adapters convert
// engine CR/CRLF line conventions before returning findings.
type ScriptPosition struct {
	Row    int `json:"row"`
	Column int `json:"column"`
}

// ScriptDiagnostic is a transport-independent snippet finding. Diagnostics may
// accompany an error when a backend completes only part of its analysis.
//
// Source names a file the engine read besides the snippet, relative to the
// request directory, when the finding is in that file rather than the
// snippet; its positions are then that file's. Fixes are suggested edits in
// snippet coordinates. Hosts accept either only from engines that declare
// the capability.
type ScriptDiagnostic struct {
	Filename       string               `json:"filename"`
	Code           string               `json:"code"`
	Name           string               `json:"name,omitempty"`
	Message        string               `json:"message"`
	Severity       string               `json:"severity,omitempty"`
	Location       ScriptPosition       `json:"location"`
	EndLocation    ScriptPosition       `json:"end_location"`
	URL            string               `json:"url,omitempty"`
	TemplateEffect ScriptTemplateEffect `json:"template_effect,omitempty"`
	Source         string               `json:"source,omitempty"`
	Fixes          []ScriptFix          `json:"fixes,omitempty"`
}

// ScriptFix is one suggested edit made of replacements in snippet coordinates.
type ScriptFix struct {
	Description  string              `json:"description,omitempty"`
	Replacements []ScriptReplacement `json:"replacements"`
}

// ScriptReplacement replaces the text between two snippet positions. Rows and
// columns follow ScriptPosition; the end is exclusive.
type ScriptReplacement struct {
	Row       int    `json:"row"`
	EndRow    int    `json:"end_row"`
	Column    int    `json:"column"`
	EndColumn int    `json:"end_column"`
	Text      string `json:"text"`
}
