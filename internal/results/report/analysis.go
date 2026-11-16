// Package report carries completed analysis values for application consumers.
package report

//go:generate go run ../../development/generators/analysis-merge -root ../../..

import (
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/input/configuration/configreport"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/outline"
)

// Analysis is the completed or partial result of an analysis session. It contains
// values only: no live rules, parser nodes, process handles, or rendering policy.
// Each published diagnostic carries the analyzed text it was reported for.
type Analysis struct {
	IncompleteChecks []IncompleteCheck             `merge:"append"`
	ActionMetadata   []actions.Coverage            `merge:"append"`
	RunnerResults    []runnerapi.Report            `merge:"append"`
	FileCount        int                           `merge:"sum"`
	Diagnostics      []results.PublishedDiagnostic `merge:"append"`
	// CapturedSources projects only admitted rows after capture teardown joins.
	// Keys are captured aliases; values and byte slices belong to this result.
	CapturedSources map[string]CapturedSource `merge:"captured"`
	Documents       outline.Outlines          `merge:"append"`
	Configurations  []configreport.Report     `merge:"append"`
	Inputs          []string                  `merge:"append"`
	Rules           []rule.Descriptor         `merge:"rules"`
	// LintedFiles lists each selected entry document with the digest of the
	// content its rules analyzed.
	LintedFiles []LintedFile `merge:"append"`
	// CollectedByRule counts, per rule that ran, the findings collected before
	// inline suppressions, rule levels and ignore settings were applied.
	CollectedByRule []RuleCollection `merge:"append"`
	// InlineSuppressions lists the rules that inline directives in analyzed
	// sources name, whether or not a finding matched them.
	InlineSuppressions []InlineSuppression `merge:"append"`
	// Providers lists the plugins and tool providers selected for the analysis
	// and whether each one took part.
	Providers []ProviderStatus `merge:"append"`
}

// LintedFile identifies one entry document by its selected path and the
// lowercase hexadecimal SHA-256 digest of the content analyzed.
type LintedFile struct {
	Path   string
	Digest string
}

// RuleCollection counts the findings one rule collected in one analysis,
// before publication policy. A rule that ran without findings has Findings 0.
type RuleCollection struct {
	Rule     string
	Findings int
}

// InlineSuppression is one rule named by an inline suppression directive.
// Path is the source as policy evaluated it; Line is 1-based.
type InlineSuppression struct {
	Path string
	Line int
	Rule string
}

// ProviderKind distinguishes plugins from the tool providers that analyze scripts.
type ProviderKind string

const (
	PluginProvider ProviderKind = "plugin"
	ToolProvider   ProviderKind = "tool"
)

// ProviderStatus records whether one plugin or tool provider took part in an
// analysis. Version and Digest identify a registry release or artifact when
// known. OnDemand marks a plugin loaded only when a script needs it. Inactive
// is empty for a provider that took part, and otherwise says why it did not,
// for example "disabled in configuration".
type ProviderStatus struct {
	Kind     ProviderKind
	Name     string
	Version  string
	Digest   string
	OnDemand bool
	Inactive string
}

// IncompleteCheck identifies work that could not finish for a captured source.
// Invocation identifies the call when known. An empty Rule means the source's
// analysis was interrupted before individual rules' completion was established.
type IncompleteCheck struct {
	Source     sourcetext.Reference `json:"source"`
	Invocation CheckInvocation      `json:"invocation,omitzero"`
	Rule       string               `json:"rule,omitempty"`
	Reason     string               `json:"reason"`
}

// CheckInvocation identifies one call within a captured entry revision. Its zero
// value means the failure occurred before an invocation could be identified.
type CheckInvocation struct {
	Entry sourcetext.Reference `json:"entry"`
	ID    invocation.ID        `json:"id"`
}

type CapturedSource struct {
	Revision             sourcetext.Revision
	Coordinates          sourcetext.Coordinates
	CoordinatesAvailable bool
	Content              []byte
}
