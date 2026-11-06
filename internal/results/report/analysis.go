// Package report carries completed analysis values for application consumers.
package report

import (
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	configreport "actionlint.kjanat.dev/v2/internal/input/configuration/report"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/input/source"
	runner "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	"actionlint.kjanat.dev/v2/internal/results"
	documentmodel "actionlint.kjanat.dev/v2/internal/results/outline"
)

// Analysis is the completed or partial result of an analysis session. It contains
// values only: no live rules, parser nodes, process handles, or rendering policy.
// Sources aligns with Diagnostics and contains the analyzed text for each finding.
type Analysis struct {
	IncompleteChecks []IncompleteCheck
	ActionMetadata   []actions.Coverage
	RunnerResults    []runner.Report
	FileCount        int
	Diagnostics      []results.Diagnostic
	Sources          []DiagnosticSource
	// CapturedSources projects only admitted rows after capture teardown joins.
	// Keys are captured aliases; values and byte slices belong to this result.
	CapturedSources map[string]CapturedSource
	Documents       documentmodel.Outlines
	Configurations  []configreport.Report
	Inputs          []string
	Rules           []rule.Descriptor
}

// IncompleteCheck records a source revision whose checks could not finish in at
// least one invocation. An empty Rule means the source's analysis was interrupted
// before completion could be established for individual rules.
type IncompleteCheck struct {
	Source source.Reference `json:"source"`
	Rule   string           `json:"rule,omitempty"`
	Reason string           `json:"reason"`
}

type CapturedSource struct {
	Revision             source.Revision
	Coordinates          source.Coordinates
	CoordinatesAvailable bool
	Content              []byte
}

// DiagnosticSource supplies source coordinates without imposing a display format.
// RawScript distinguishes external tools' LF coordinates from YAML line breaks.
// Unavailable forbids inferring ranges from unverified display text.
type DiagnosticSource struct {
	Content     []byte
	RawScript   bool
	Unavailable bool
	// Suppression is present only when the resolved policy permits a comment
	// for this rule. Editors bind its YAML target to a captured revision.
	Suppression *SuppressionTarget
}

// SuppressionTarget identifies the YAML declaration that may suppress a finding.
type SuppressionTarget struct {
	Path        string
	First, Last int
}
