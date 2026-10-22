package checks

import (
	jsonv2 "encoding/json/v2"
	"fmt"

	"actionlint.kjanat.dev/analyzer"
	repoconfig "actionlint.kjanat.dev/internal/config"
)

// ConfigureAnalyzers applies presets and per-rule settings to explicit registrations.
// still apply. Library registration options take precedence over repository options.
func ConfigureAnalyzers(config *repoconfig.Config, registrations []analyzer.Registration) ([]analyzer.Registration, error) {
	selected := make([]analyzer.Registration, 0, len(registrations))
	for _, registration := range registrations {
		name := registration.Analyzer.Descriptor().Name
		if repoconfig.DiagnosticLevel(config, name) == "off" {
			continue
		}
		if config != nil && len(registration.Options) == 0 {
			if options := config.Lint.Rules.Plugins.Rules[name].Options; options != nil {
				data, err := jsonv2.Marshal(options)
				if err != nil {
					return nil, fmt.Errorf("options for analyzer %q: %w", name, err)
				}
				registration.Options = data
			}
		}
		selected = append(selected, registration)
	}
	return selected, nil
}
