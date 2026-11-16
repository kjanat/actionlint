// Package report carries completed analysis values for application consumers.
package report

//go:generate go run ../../development/generators/analysis-merge -root ../../..

import (
	"actionlint.kjanat.dev/v2/internal/input/configuration/configreport"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/input/source"
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
}

// IncompleteCheck identifies work that could not finish for a captured source.
// Invocation identifies the call when known. An empty Rule means the source's
// analysis was interrupted before individual rules' completion was established.
type IncompleteCheck struct {
	Source     source.Reference `json:"source"`
	Invocation CheckInvocation  `json:"invocation,omitzero"`
	Rule       string           `json:"rule,omitempty"`
	Reason     string           `json:"reason"`
}

// CheckInvocation identifies one call within a captured entry revision. Its zero
// value means the failure occurred before an invocation could be identified.
type CheckInvocation struct {
	Entry source.Reference `json:"entry"`
	ID    invocation.ID    `json:"id"`
}

type CapturedSource struct {
	Revision             source.Revision
	Coordinates          source.Coordinates
	CoordinatesAvailable bool
	Content              []byte
}
