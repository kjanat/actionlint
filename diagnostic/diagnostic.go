// Package diagnostic defines findings and source edits shared by analysis and parsing.
package diagnostic

import "actionlint.kjanat.dev/v2/source"

// Position uses one-based Unicode character positions. Range ends are exclusive and may lie on a later line.
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// Diagnostic is the structured CLI representation, separate from legacy template fields.
type Diagnostic struct {
	Reference        *source.Reference `json:"reference,omitempty"`
	Explanation      string            `json:"explanation,omitempty"`
	Help             string            `json:"help,omitempty"`
	DocumentationURL string            `json:"documentation_url,omitempty"`
	Related          []Related         `json:"related,omitempty"`

	Rule string `json:"rule"`
	// Code identifies a diagnostic kind within Rule, for native and external checks.
	// Severity records its effective level. Empty values mean metadata was not supplied.
	Code     string   `json:"code,omitempty"`
	Severity string   `json:"severity,omitempty"`
	Message  string   `json:"message"`
	Path     string   `json:"path"`
	Start    Position `json:"start"`
	End      Position `json:"end"`
	Snippet  string   `json:"snippet,omitempty"`
	Fixes    []Fix    `json:"fixes,omitempty"`
}

// Fix is one complete suggested change. Apply all edits together.
type Fix struct {
	// Applicability is safe, unsafe or suggested. Missing values require review.
	Applicability string `json:"applicability,omitempty"`
	// Revisions bind edits to the source snapshots the analyzer checked.
	Revisions map[string]string `json:"revisions,omitempty"`

	Description string `json:"description"`
	Edits       []Edit `json:"edits"`
}

// Edit replaces a half-open Unicode range in the original source file.
type Edit struct {
	Path        string   `json:"path"`
	Start       Position `json:"start"`
	End         Position `json:"end"`
	Replacement string   `json:"replacement"`
}

// Related identifies another declaration relevant to a finding.
type Related struct {
	Message string   `json:"message"`
	Path    string   `json:"path"`
	Start   Position `json:"start"`
	End     Position `json:"end"`
}
