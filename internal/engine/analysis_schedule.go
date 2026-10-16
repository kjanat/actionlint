package engine

import (
	"context"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/analyzerhost"
)

// runScriptAnalyzers schedules prepared work without knowing how scripts were discovered.
func runScriptAnalyzers(ctx context.Context, analyzers []scriptAnalyzer, batch scriptBatch) ([]*Error, error) {
	tasks := make([]analyzerhost.Task[*Error], len(analyzers))
	for i, builtin := range analyzers {
		tasks[i] = func(ctx context.Context) ([]*Error, error) { return builtin.AnalyzeScripts(ctx, batch) }
	}
	return analyzerhost.RunTasks(ctx, tasks)
}
func runDocumentAnalyzers(ctx context.Context, registrations []analyzer.Registration, input analyzer.Input, metadataSources map[string][]byte, usedRules *[]RuleInfo) ([]*Error, error) {
	var all []*Error
	var analysisErr error
	if len(registrations) != 0 {
		findings, err := analyzerhost.Run(ctx, input, registrations)
		analysisErr = err
		for _, registration := range registrations {
			if registration.Analyzer != nil {
				*usedRules = append(*usedRules, RuleInfo{Name: registration.Analyzer.Descriptor().Name})
			}
		}
		for _, finding := range findings {
			legacy := legacyDiagnosticError(finding)
			if legacy.Filepath != "" && legacy.Filepath != input.Path {
				if source, ok := metadataSources[legacy.Filepath]; ok {
					legacy.source = source
				}
			}
			all = append(all, legacy)
		}
	}

	return all, analysisErr
}
