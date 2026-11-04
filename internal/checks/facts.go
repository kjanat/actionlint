package checks

import (
	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/analyzerref"
)

func RuleNames(registrations []analyzer.Registration) []string {
	var names []string
	for _, registration := range registrations {
		if !analyzerref.IsNil(registration.Analyzer) {
			names = append(names, registration.Analyzer.Descriptor().Name)
		}
	}
	return names
}

func RequiredFacts(registrations []analyzer.Registration) map[analyzer.InputKind]bool {
	facts := map[analyzer.InputKind]bool{}
	for _, registration := range registrations {
		if analyzerref.IsNil(registration.Analyzer) {
			continue
		}
		for _, fact := range registration.Analyzer.Descriptor().RequiredInputs {
			facts[fact] = true
		}
	}
	return facts
}
