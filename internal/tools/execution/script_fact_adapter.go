package execution

import (
	"context"

	"actionlint.kjanat.dev/v2/analyzer"
	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
)

type FactAnalyzer struct{ Analyzer analyzer.Analyzer }

func (adapter FactAnalyzer) AnalyzeScripts(ctx context.Context, batch Batch) ([]*findingmodel.Finding, error) {
	if !batch.Valid() {
		return nil, ErrInvalidBatch
	}
	var findings []*findingmodel.Finding
	for index, unit := range batch.Scripts() {
		if unit.Input != nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return findings, err
		}
		script, _ := batch.Fact(index, false)
		input := analyzer.Input{Scripts: []analyzer.Script{script}}
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
