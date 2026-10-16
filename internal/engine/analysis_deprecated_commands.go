package engine

import (
	"context"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/workflowcommands"
)

type deprecatedCommandsAnalyzer struct{}

func (deprecatedCommandsAnalyzer) AnalyzeScripts(ctx context.Context, batch scriptBatch) ([]*Error, error) {
	var findings []*Error
	for _, unit := range batch.scripts {
		if err := ctx.Err(); err != nil {
			return findings, err
		}
		input := analyzer.Input{Scripts: []analyzer.Script{scriptFact(unit, unit.path, unit.path, false)}}
		diagnostics, err := workflowcommands.Analyzer{}.Analyze(ctx, input)
		for _, d := range diagnostics {
			finding := legacyDiagnosticError(d)
			if d.End == (diagnostic.Position{}) {
				finding.endPosition = nil
			}
			finding.Filepath, finding.source, finding.ignoreOrigin = unit.path, unit.content, unit.origin
			findings = append(findings, finding)
		}
		if err != nil {
			return findings, err
		}
	}
	return findings, nil
}
