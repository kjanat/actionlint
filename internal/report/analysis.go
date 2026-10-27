// Package report carries completed analysis values for application consumers.
package report

import (
	"actionlint.kjanat.dev/v2/diagnostic"
	documentmodel "actionlint.kjanat.dev/v2/document"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/rule"
	"actionlint.kjanat.dev/v2/runner"
	"actionlint.kjanat.dev/v2/source"
)

// Analysis is the completed or partial result of an analysis session. It contains
// values only: no live rules, parser nodes, process handles, or rendering policy.
// Sources aligns with Diagnostics and contains the analyzed text for each finding.
type Analysis struct {
	RunnerEvidence []runner.Report
	FileCount      int
	Diagnostics    []diagnostic.Diagnostic
	Sources        []DiagnosticSource
	// CapturedSources projects only admitted rows after capture teardown joins.
	// Keys are captured aliases; values and byte slices belong to this result.
	CapturedSources map[string]CapturedSource
	Documents       documentmodel.Outlines
	Configurations  []repoconfig.Report
	Inputs          []string
	Rules           []rule.Descriptor
}

type CapturedSource struct {
	Revision             source.Revision
	Coordinates          source.Coordinates
	CoordinatesAvailable bool
	Content              []byte
}

// DiagnosticSource supplies source coordinates without imposing a display format.
// RawScript distinguishes external tools' LF coordinates from YAML line breaks.
type DiagnosticSource struct {
	Content     []byte
	RawScript   bool
	Unavailable bool
}
