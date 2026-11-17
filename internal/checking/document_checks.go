package checking

import (
	"context"

	"actionlint.kjanat.dev/v2/internal/checking/executor"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

func runDocumentAnalyzers(ctx context.Context, registrations []results.AnalyzerRegistration, input checkinputs.AnalyzerInput, metadataSources map[string][]byte, usedRules *[]rule.Descriptor) ([]*finding.Finding, error) {
	var all []*finding.Finding
	var analysisErr error
	if len(registrations) != 0 {
		findings, err := executor.Run(ctx, input, registrations)
		analysisErr = err
		for _, registration := range registrations {
			if !results.IsNilAnalyzer(registration.Analyzer) {
				*usedRules = append(*usedRules, registration.Analyzer.Descriptor().Descriptor.Clone())
			}
		}
		for _, item := range findings {
			legacy := finding.FromDiagnostic(item)
			if legacy.Path != "" && legacy.Path != input.Path {
				if source, ok := metadataSources[legacy.Path]; ok {
					legacy.Source.Content = source
				}
			}
			all = append(all, legacy)
		}
	}

	return all, analysisErr
}
