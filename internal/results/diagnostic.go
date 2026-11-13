// Package results defines findings and proposed source edits.
package results

import sourcepos "actionlint.kjanat.dev/v2/internal/input/source"

// Diagnostic is the structured CLI representation, separate from legacy template fields.
type Diagnostic struct {
	// NonBlocking findings remain visible but do not make the check exit with status 1.
	NonBlocking      bool                 `json:"non_blocking,omitempty"`
	Reference        *sourcepos.Reference `json:"reference,omitempty"`
	Explanation      string               `json:"explanation,omitempty"`
	Help             string               `json:"help,omitempty"`
	DocumentationURL string               `json:"documentation_url,omitempty"`
	Related          []Related            `json:"related,omitempty"`

	Rule string `json:"rule"`
	// Code identifies a diagnostic kind within Rule, for native and external checks.
	// Severity records its effective level. Empty values mean metadata was not supplied.
	Code     string             `json:"code,omitempty"`
	Severity string             `json:"severity,omitempty"`
	Message  string             `json:"message"`
	Path     string             `json:"path"`
	Start    sourcepos.Position `json:"start"`
	End      sourcepos.Position `json:"end"`
	Snippet  string             `json:"snippet,omitempty"`
	Fixes    []Fix              `json:"fixes,omitempty"`
}

// Fix is one complete suggested change. Apply all edits together.
type Fix struct {
	// Applicability belongs to this alternative, not its rule. Missing values require review.
	Applicability Applicability `json:"applicability,omitempty"`
	// Revisions bind edits to the source snapshots the analyzer checked.
	Revisions map[string]string `json:"revisions,omitempty"`

	Description string `json:"description"`
	Edits       []Edit `json:"edits"`
}

// Edit replaces a half-open Unicode range in the original source file.
type Edit struct {
	Path        string             `json:"path"`
	Start       sourcepos.Position `json:"start"`
	End         sourcepos.Position `json:"end"`
	Replacement string             `json:"replacement"`
}

// Related identifies another declaration relevant to a finding.
type Related struct {
	Revision    sourcepos.Revision    `json:"revision,omitzero"`
	Coordinates sourcepos.Coordinates `json:"coordinates,omitempty"`
	Message     string                `json:"message"`
	Path        string                `json:"path"`
	Start       sourcepos.Position    `json:"start"`
	End         sourcepos.Position    `json:"end"`
}
