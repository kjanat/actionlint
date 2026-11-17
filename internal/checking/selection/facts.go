package selection

import (
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs/validation"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

func RuleNames(registrations []checkinputs.Registration) []string {
	var names []string
	for _, registration := range registrations {
		if !validation.IsNil(registration.Checker) {
			names = append(names, registration.Checker.Descriptor().Name)
		}
	}
	return names
}

func RequiredFacts(registrations []checkinputs.Registration) map[rule.InputKind]bool {
	facts := map[rule.InputKind]bool{}
	for _, registration := range registrations {
		if validation.IsNil(registration.Checker) {
			continue
		}
		for _, fact := range registration.Checker.Descriptor().RequiredInputs {
			facts[fact] = true
		}
	}
	return facts
}
