package checking

import (
	"errors"
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/internal/checking/executor"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/results"
)

// ConfigureToolIntegrations replaces registered tools using declared contracts.
// occupiedTools identifies already-selected script-engine providers. Integrated
// analyzers are removed from standalone execution, making repeated calls safe.
func ConfigureToolIntegrations(options Options, occupiedTools ...string) (Options, error) {
	if err := validateAnalyzerRegistrations(options.Analyzers); err != nil {
		return options, err
	}
	options.Tools = slices.Clone(options.Tools)
	remaining := make([]results.AnalyzerRegistration, 0, len(options.Analyzers))
	seen := make(map[string]bool, len(occupiedTools))
	for _, name := range occupiedTools {
		seen[name] = true
	}
	for _, registration := range options.Tools {
		if integration := registration.ToolIntegration; integration != nil {
			if seen[integration.Tool] {
				return options, fmt.Errorf("multiple providers for tool integration %q", integration.Tool)
			}
			seen[integration.Tool] = true
		}
	}
	for _, registration := range options.Analyzers {
		integrator, ok := registration.Analyzer.(checkinputs.ToolIntegrator)
		if !ok || integrator.ToolIntegration() == nil {
			remaining = append(remaining, registration)
			continue
		}
		descriptor := integrator.ToolIntegration()
		if len(registration.Options) != 0 {
			if err := executor.ValidateOptions(registration.Analyzer.Descriptor().OptionsSchema, registration.Options); err != nil {
				return options, fmt.Errorf("analyzer %q options: %w", descriptor.Tool, err)
			}
		}
		if descriptor.Tool == "" || registration.Analyzer.Descriptor().Name != descriptor.Tool {
			return options, errors.New("tool integration identifier must match analyzer rule identity")
		}
		if seen[descriptor.Tool] {
			return options, fmt.Errorf("multiple providers for tool integration %q", descriptor.Tool)
		}
		seen[descriptor.Tool] = true
		found := false
		for i, tool := range options.Tools {
			if tool.Name != descriptor.Tool {
				continue
			}
			if tool.WithAnalyzer == nil {
				return options, fmt.Errorf("tool %q cannot use a document analyzer", descriptor.Tool)
			}
			replacement, err := tool.WithAnalyzer(results.AnalyzerRegistration{Analyzer: registration.Analyzer, Options: slices.Clone(registration.Options)})
			if err != nil {
				return options, err
			}
			options.Tools[i] = replacement
			found = true
			break
		}
		if !found {
			return options, fmt.Errorf("managed plugin requires tool %q", descriptor.Tool)
		}
	}
	options.Analyzers = remaining
	return options, nil
}

func validateAnalyzerRegistrations(registrations []results.AnalyzerRegistration) error {
	for _, registration := range registrations {
		if results.IsNilAnalyzer(registration.Analyzer) {
			return errors.New("nil analyzer registration")
		}
	}
	return nil
}
