package checking

import (
	"context"

	checkexecutor "actionlint.kjanat.dev/v2/internal/checking/executor"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/inputs/validation"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

func runDocumentAnalyzers(ctx context.Context, registrations []checkinputs.Registration, input checkinputs.AnalyzerInput, metadataSources map[string][]byte, usedRules *[]rule.Descriptor) ([]*findingmodel.Finding, error) {
	var all []*findingmodel.Finding
	var analysisErr error
	if len(registrations) != 0 {
		findings, err := checkexecutor.Run(ctx, input, registrations)
		analysisErr = err
		for _, registration := range registrations {
			if !validation.IsNil(registration.Checker) {
				*usedRules = append(*usedRules, registration.Checker.Descriptor().Descriptor.Clone())
			}
		}
		for _, finding := range findings {
			legacy := findingmodel.FromDiagnostic(finding)
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
