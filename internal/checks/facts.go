package checks

import "actionlint.kjanat.dev/analyzer"

func RuleNames(registrations []analyzer.Registration) []string {
	var names []string
	for _, registration := range registrations {
		if registration.Analyzer != nil {
			names = append(names, registration.Analyzer.Descriptor().Name)
		}
	}
	return names
}

func RequiredFacts(registrations []analyzer.Registration) map[analyzer.Fact]bool {
	facts := map[analyzer.Fact]bool{}
	for _, registration := range registrations {
		if registration.Analyzer == nil {
			continue
		}
		for _, fact := range registration.Analyzer.Descriptor().Requires {
			facts[fact] = true
		}
	}
	return facts
}
