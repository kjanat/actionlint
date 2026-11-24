// Package results defines findings and proposed source edits.
package results

import (
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/applicability"
	"actionlint.kjanat.dev/v2/internal/model/severity"
)

type DiagnosticOrigin string

const (
	ProjectOrigin    DiagnosticOrigin = "project"
	DependencyOrigin DiagnosticOrigin = "dependency"
)

type Diagnostic struct {
	Origin DiagnosticOrigin `json:"origin,omitempty"`
	// NonBlocking findings remain visible but do not make the check exit with status 1.
	NonBlocking      bool                  `json:"non_blocking,omitempty"`
	Reference        *sourcetext.Reference `json:"reference,omitempty"`
	Explanation      string                `json:"explanation,omitempty"`
	Help             string                `json:"help,omitempty"`
	DocumentationURL string                `json:"documentation_url,omitempty"`
	Related          []Related             `json:"related,omitempty"`
	Lifecycle        *LifecycleReport      `json:"lifecycle,omitempty"`

	Rule string `json:"rule"`
	// Empty Code and Severity values mean metadata was not supplied.
	Code     string              `json:"code,omitempty"`
	Severity severity.Severity   `json:"severity,omitempty"`
	Message  string              `json:"message"`
	Path     string              `json:"path"`
	Start    sourcetext.Position `json:"start"`
	End      sourcetext.Position `json:"end"`
	Snippet  string              `json:"snippet,omitempty"`
	Fixes    []Fix               `json:"fixes,omitempty"`
}

type LifecycleReport struct {
	Subject     string                  `json:"subject"`
	Name        string                  `json:"name"`
	Event       string                  `json:"event"`
	Source      string                  `json:"source"`
	AnnouncedOn string                  `json:"announced_on,omitempty"`
	EffectiveOn string                  `json:"effective_on,omitempty"`
	AsOf        string                  `json:"as_of"`
	Policies    []LifecyclePolicyReport `json:"policies"`
}

type LifecyclePolicyReport struct {
	ID          string            `json:"id"`
	Description string            `json:"description"`
	Origin      string            `json:"origin"`
	Severity    severity.Severity `json:"severity"`
	StartsOn    string            `json:"starts_on"`
	Enforced    bool              `json:"enforced"`
}

// Fix edits must be applied together.
type Fix struct {
	// Missing values require review.
	Applicability applicability.Applicability `json:"applicability,omitempty"`
	// Revisions bind edits to the source snapshots the analyzer checked.
	Revisions map[string]string `json:"revisions,omitempty"`

	Description string `json:"description"`
	Edits       []Edit `json:"edits"`
}

// Edit replaces a half-open Unicode range in the original source file.
type Edit struct {
	Path        string              `json:"path"`
	Start       sourcetext.Position `json:"start"`
	End         sourcetext.Position `json:"end"`
	Replacement string              `json:"replacement"`
}

type Related struct {
	Revision    sourcetext.Revision    `json:"revision,omitzero"`
	Coordinates sourcetext.Coordinates `json:"coordinates,omitempty"`
	Message     string                 `json:"message"`
	Path        string                 `json:"path"`
	Start       sourcetext.Position    `json:"start"`
	End         sourcetext.Position    `json:"end"`
}
