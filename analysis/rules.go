package analysis

import (
	"errors"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/rulecatalog"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
	"actionlint.kjanat.dev/v2/rule"
)

// Rules returns built-in and registered analyzer metadata in name order. A
// negotiated tool integration replaces that tool's metadata; duplicate providers
// and unsupported integration profiles are rejected as they are during analysis.
func Rules(extensions ...analyzer.Analyzer) ([]rule.Descriptor, error) {
	registrations := make([]analyzer.Registration, 0, len(extensions))
	for _, extension := range extensions {
		if extension == nil {
			return nil, errors.New("nil analyzer registration")
		}
		registrations = append(registrations, analyzer.Registration{Analyzer: extension})
	}
	configured, err := application.ConfigureToolIntegrations(application.Options{Analyzers: registrations, Tools: adapters.Options{ShellCheck: "shellcheck", Ruff: "ruff"}.Registrations()})
	if err != nil {
		return nil, err
	}
	descriptors := make([]rule.Descriptor, 0, len(configured.Analyzers))
	for _, registration := range configured.Analyzers {
		descriptors = append(descriptors, registration.Analyzer.Descriptor().Descriptor)
	}
	result, err := rulecatalog.Combine(descriptors...)
	if err != nil {
		return nil, err
	}
	for _, extension := range extensions {
		integrator, ok := extension.(analyzer.ToolIntegrator)
		if !ok || integrator.ToolIntegration() == nil {
			continue
		}
		descriptor := extension.Descriptor().Descriptor
		for i := range result {
			if result[i].Name == descriptor.Name {
				result[i] = descriptor
				break
			}
		}
	}
	return result, nil
}
