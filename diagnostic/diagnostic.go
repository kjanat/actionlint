// Package diagnostic defines findings and source edits shared by analysis and parsing.
package diagnostic

// Position uses one-based Unicode character positions. Range ends are exclusive and may lie on a later line.
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// Diagnostic is the structured CLI representation, separate from legacy template fields.
type Diagnostic struct {
	Rule string `json:"rule"`
	// Code and Severity retain an external analyzer's identifier and native level.
	// Empty values mean the rule did not supply this metadata.
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
