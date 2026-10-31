package checks

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/analyzerref"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
)

// ConfigureAnalyzers applies presets and per-rule settings to explicit registrations.
// still apply. Library registration options take precedence over repository options.
func ConfigureAnalyzers(config *repoconfig.Config, registrations []analyzer.Registration) ([]analyzer.Registration, error) {
	selected := make([]analyzer.Registration, 0, len(registrations))
	for _, registration := range registrations {
		if analyzerref.IsNil(registration.Analyzer) {
			return nil, errors.New("nil analyzer registration")
		}
		name := registration.Analyzer.Descriptor().Name
		if repoconfig.DiagnosticLevel(config, name) == "off" {
			continue
		}
		if config != nil && len(registration.Options) == 0 {
			options, err := config.Lint.Rules.PluginOptions(name)
			if err != nil {
				return nil, err
			}
			if options != nil {
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
