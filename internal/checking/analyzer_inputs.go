package checking

import (
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

// requiredInputs returns the input kinds that registrations require.
func requiredInputs(registrations []checkinputs.Registration) map[rule.InputKind]bool {
	inputs := map[rule.InputKind]bool{}
	for _, registration := range registrations {
		if checkinputs.IsNilAnalyzer(registration.Analyzer) {
			continue
		}
		for _, input := range registration.Analyzer.Descriptor().RequiredInputs {
			inputs[input] = true
		}
	}
	return inputs
}
