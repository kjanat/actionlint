package selection

import (
	"errors"

	"actionlint.kjanat.dev/v2/internal/input/configuration/rulebinding"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
)

// ConfigureAnalyzers applies presets and per-rule settings to explicit registrations.
// Library registration options take precedence over repository options.
func ConfigureAnalyzers(scope *rulebinding.Scope, registrations []checkinputs.Registration) ([]checkinputs.Registration, error) {
	selected := make([]checkinputs.Registration, 0, len(registrations))
	for _, registration := range registrations {
		if checkinputs.IsNilAnalyzer(registration.Analyzer) {
			return nil, errors.New("nil analyzer registration")
		}
		name := registration.Analyzer.Descriptor().Name
		config := scope.Rule(name).Config()
		if config.RuleSet().DiagnosticLevel(config, name) == "off" {
			continue
		}
		options, err := scope.Rule(name).Options(registration.Options)
		if err != nil {
			return nil, err
		}
		registration.Options = options
		selected = append(selected, registration)
	}
	return selected, nil
}
