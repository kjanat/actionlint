package output

import (
	"actionlint.kjanat.dev/v2/internal/results/report"
)

// Result supplies report data and captured snippets for rendering. The caller
// sets command completion in CheckResult before requesting JSON output.
type Result struct {
	report.CheckResult
	Findings []Finding
	Rules    []Rule
}

// FromAnalysis prepares display data without choosing command status. It converts
// half-open source spans to inclusive columns for text and template output.
func FromAnalysis(analysis report.Analysis) Result {
	r := Result{
		CheckResult: report.FromAnalysis(analysis),
		Rules:       analysis.Rules,
	}
	for i, d := range analysis.Diagnostics {
		var source report.DiagnosticSource
		if i < len(analysis.Sources) {
			source = analysis.Sources[i]
		}
		finding := FindingOf(d, source.Content)
		finding.RawScript = source.RawScript
		finding.Unavailable = source.Unavailable
		r.Findings = append(r.Findings, finding)
	}
	return r
}
