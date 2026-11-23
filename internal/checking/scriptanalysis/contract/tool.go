package contract

import (
	"context"

	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

type Tool struct {
	Name   string
	Runner Runner
}

func (tool Tool) AnalyzeScripts(ctx context.Context, batch scriptanalysis.Batch) ([]*finding.Finding, error) {
	if !batch.Valid() {
		return nil, scriptanalysis.ErrInvalidBatch
	}
	prepared, err := batch.ForTool(tool.Name)
	if err != nil {
		return nil, err
	}
	for index := range prepared.Scripts {
		if _, exists := prepared.Fact(index, true); !exists {
			return nil, scriptanalysis.ErrInvalidBatch
		}
	}
	findings, err := tool.Runner.Analyze(ctx, prepared)
	out := make([]*finding.Finding, 0, len(findings))
	for _, result := range findings {
		finding := finding.FromDiagnostic(result.Diagnostic)
		finding.Source = result.Source
		finding.SuppressionOrigin = result.Suppression
		if result.Script >= 0 && result.Script < len(batch.Scripts) {
			script := batch.Scripts[result.Script]
			if scope := script.Scope.Bindings; scope != nil {
				finding.Binding = scope.Rule(finding.Rule).ID()
			}
			finding.IgnoreOrigin = script.Origin
			finding.Owner = script.Owner
		}
		out = append(out, finding)
	}
	return out, err
}
