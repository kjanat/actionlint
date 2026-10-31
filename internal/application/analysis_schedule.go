package application

import (
	"context"

	"actionlint.kjanat.dev/v2/rule"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"actionlint.kjanat.dev/v2/internal/analyzerref"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
)

func runDocumentAnalyzers(ctx context.Context, registrations []analyzer.Registration, input analyzer.Input, metadataSources map[string][]byte, usedRules *[]rule.Descriptor) ([]*findingmodel.Finding, error) {
	var all []*findingmodel.Finding
	var analysisErr error
	if len(registrations) != 0 {
		findings, err := analyzerhost.Run(ctx, input, registrations)
		analysisErr = err
		for _, registration := range registrations {
			if !analyzerref.IsNil(registration.Analyzer) {
				*usedRules = append(*usedRules, registration.Analyzer.Descriptor().Clone())
			}
		}
		for _, finding := range findings {
			legacy := findingmodel.FromDiagnostic(finding)
			if legacy.Path != "" && legacy.Path != input.Path {
				if source, ok := metadataSources[legacy.Path]; ok {
					legacy.Source = source
				}
			}
			all = append(all, legacy)
		}
	}

	return all, analysisErr
}
