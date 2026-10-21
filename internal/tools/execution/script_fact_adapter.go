package execution

import (
	"context"

	"actionlint.kjanat.dev/analyzer"
	diagmodel "actionlint.kjanat.dev/diagnostic"
	findingmodel "actionlint.kjanat.dev/internal/finding"
)

type FactAnalyzer struct{ Analyzer analyzer.Analyzer }

func (adapter FactAnalyzer) AnalyzeScripts(ctx context.Context, batch Batch) ([]*findingmodel.Finding, error) {
	var findings []*findingmodel.Finding
	for _, unit := range batch.Scripts {
		if unit.Input != nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return findings, err
		}
		input := analyzer.Input{Scripts: []analyzer.Script{Fact(unit, unit.Path, unit.Path, false)}}
		diagnostics, err := adapter.Analyzer.Analyze(ctx, input)
		for _, d := range diagnostics {
			finding := findingmodel.FromDiagnostic(d)
			if d.End == (diagmodel.Position{}) {
				finding.EndPosition = nil
			}
			finding.Path, finding.Source, finding.IgnoreOrigin, finding.Owner = unit.Path, unit.Content, unit.Origin, unit.Owner
			findings = append(findings, finding)
		}
		if err != nil {
			return findings, err
		}
	}
	return findings, nil
}
