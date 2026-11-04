package checks

import (
	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/analyzerref"
)

func RuleNames(registrations []analyzer.Registration) []string {
	var names []string
	for _, registration := range registrations {
		if !analyzerref.IsNil(registration.Checker) {
			names = append(names, registration.Checker.Descriptor().Name)
		}
	}
	return names
}

func RequiredFacts(registrations []analyzer.Registration) map[analyzer.InputKind]bool {
	facts := map[analyzer.InputKind]bool{}
	for _, registration := range registrations {
		if analyzerref.IsNil(registration.Checker) {
			continue
		}
		for _, fact := range registration.Checker.Descriptor().RequiredInputs {
			facts[fact] = true
		}
	}
	return facts
}
