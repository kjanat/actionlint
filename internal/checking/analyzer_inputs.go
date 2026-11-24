package checking

import (
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/results"
)

func requiredInputs(registrations []results.AnalyzerRegistration) map[rule.InputKind]bool {
	inputs := map[rule.InputKind]bool{}
	for _, registration := range registrations {
		if results.IsNilAnalyzer(registration.Analyzer) {
			continue
		}
		for _, input := range registration.Analyzer.Descriptor().RequiredInputs {
			inputs[input] = true
		}
	}
	return inputs
}
