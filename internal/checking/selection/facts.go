package selection

import (
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/inputs/validation"
	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"
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

func RequiredFacts(registrations []checkinputs.Registration) map[rulemeta.InputKind]bool {
	facts := map[rulemeta.InputKind]bool{}
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
