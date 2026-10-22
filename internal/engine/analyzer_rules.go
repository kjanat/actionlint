package engine

import "actionlint.kjanat.dev/analyzer"

func analyzerRuleNames(registrations []analyzer.Registration) []string {
	names := make([]string, 0, len(registrations))
	for _, registration := range registrations {
		if registration.Analyzer != nil {
			names = append(names, registration.Analyzer.Descriptor().Name)
		}
	}
	return names
}
