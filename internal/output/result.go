package output

import (
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

// RenderedReport supplies report data and published findings for rendering. The
// caller sets command completion in CheckResult before requesting JSON output.
type RenderedReport struct {
	report.CheckResult
	Findings []results.PublishedDiagnostic
	Rules    []rule.Descriptor
}

// FromAnalysis prepares display data without choosing command status.
func FromAnalysis(analysis report.Analysis) RenderedReport {
	return RenderedReport{
		CheckResult: report.FromAnalysis(analysis),
		Findings:    analysis.Diagnostics,
		Rules:       analysis.Rules,
	}
}
