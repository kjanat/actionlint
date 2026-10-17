package engine

import (
	"context"

	"actionlint.kjanat.dev/analyzer"
	diagmodel "actionlint.kjanat.dev/diagnostic"
	findingmodel "actionlint.kjanat.dev/internal/finding"
)

type factScriptAnalyzer struct{ analyzer analyzer.Analyzer }

func (adapter factScriptAnalyzer) AnalyzeScripts(ctx context.Context, batch scriptBatch) ([]*findingmodel.Finding, error) {
	var findings []*findingmodel.Finding
	for _, unit := range batch.scripts {
		if err := ctx.Err(); err != nil {
			return findings, err
		}
		input := analyzer.Input{Scripts: []analyzer.Script{scriptFact(unit, unit.path, unit.path, false)}}
		diagnostics, err := adapter.analyzer.Analyze(ctx, input)
		for _, d := range diagnostics {
			finding := findingmodel.FromDiagnostic(d)
			if d.End == (diagmodel.Position{}) {
				finding.EndPosition = nil
			}
			finding.Path, finding.Source, finding.IgnoreOrigin = unit.path, unit.content, unit.origin
			findings = append(findings, finding)
		}
		if err != nil {
			return findings, err
		}
	}
	return findings, nil
}
