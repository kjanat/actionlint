// Package report carries completed analysis values for application consumers.
package report

import (
	"actionlint.kjanat.dev/v2/diagnostic"
	documentmodel "actionlint.kjanat.dev/v2/document"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/rule"
	"actionlint.kjanat.dev/v2/runner"
)

// Analysis is the completed or partial result of an analysis session. It contains
// values only: no live rules, parser nodes, process handles, or rendering policy.
// Sources is aligned with Diagnostics and retains the exact analyzed text, including
// dependencies. Consumers may change displayed diagnostic paths without losing it.
type Analysis struct {
	RunnerEvidence []runner.Report
	FileCount      int
	Diagnostics    []diagnostic.Diagnostic
	Sources        []DiagnosticSource
	Documents      documentmodel.Outlines
	Configurations []repoconfig.Report
	Inputs         []string
	Rules          []rule.Descriptor
}

// DiagnosticSource supplies source coordinates without imposing a display format.
// RawScript distinguishes external tools' LF coordinates from YAML line breaks.
type DiagnosticSource struct {
	Content   []byte
	RawScript bool
}
