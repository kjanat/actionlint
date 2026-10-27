package checks

import (
	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
)

func RuleNames(registrations []analyzer.Registration) []string {
	var names []string
	for _, registration := range registrations {
		if !analyzerhost.IsNilAnalyzer(registration.Analyzer) {
			names = append(names, registration.Analyzer.Descriptor().Name)
		}
	}
	return names
}

func RequiredFacts(registrations []analyzer.Registration) map[analyzer.Fact]bool {
	facts := map[analyzer.Fact]bool{}
	for _, registration := range registrations {
		if analyzerhost.IsNilAnalyzer(registration.Analyzer) {
			continue
		}
		for _, fact := range registration.Analyzer.Descriptor().Requires {
			facts[fact] = true
		}
	}
	return facts
}
