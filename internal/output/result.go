package output

import (
	"actionlint.kjanat.dev/v2/internal/results/report"
)

// RenderedReport supplies report data and captured snippets for rendering. The caller
// sets command completion in CheckResult before requesting JSON output.
type RenderedReport struct {
	report.CheckResult
	Findings []RenderedFinding
	Rules    []Rule
}

// FromAnalysis prepares display data without choosing command status. It converts
// half-open source spans to inclusive columns for text and template output.
func FromAnalysis(analysis report.Analysis) RenderedReport {
	r := RenderedReport{
		CheckResult: report.FromAnalysis(analysis),
		Rules:       analysis.Rules,
	}
	for i, d := range analysis.Diagnostics {
		finding := RenderedFinding{Diagnostic: d, canonicalSnippet: d.Snippet}
		if i < len(analysis.Sources) {
			finding.Source = analysis.Sources[i]
		}
		r.Findings = append(r.Findings, finding)
	}
	return r
}
