package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"actionlint.kjanat.dev/v2/internal/analyzerref"
)

// ConfigureToolIntegrations replaces registered tools using declared contracts.
// occupiedTools identifies already-selected script-engine providers. Integrated
// analyzers are removed from standalone execution, making repeated calls safe.
func ConfigureToolIntegrations(options Options, occupiedTools ...string) (Options, error) {
	for _, registration := range options.Analyzers {
		if analyzerref.IsNil(registration.Analyzer) {
			return options, errors.New("nil analyzer registration")
		}
	}
	options.Tools = slices.Clone(options.Tools)
	remaining := make([]analyzer.Registration, 0, len(options.Analyzers))
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
		integrator, ok := registration.Analyzer.(analyzer.ToolIntegrator)
		if !ok || integrator.ToolIntegration() == nil {
			remaining = append(remaining, registration)
			continue
		}
		descriptor := integrator.ToolIntegration()
		if len(registration.Options) != 0 {
			if err := analyzerhost.ValidateOptions(registration.Analyzer.Descriptor().OptionsSchema, registration.Options); err != nil {
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
			if tool.Descriptor.Name != descriptor.Tool {
				continue
			}
			if tool.WithAnalyzer == nil {
				return options, fmt.Errorf("tool %q cannot use a document analyzer", descriptor.Tool)
			}
			bound := boundToolAnalyzer{Analyzer: registration.Analyzer, integrator: integrator, options: slices.Clone(registration.Options)}
			replacement, err := tool.WithAnalyzer(bound)
			if err != nil {
				return options, err
			}
			declared := *descriptor
			replacement.ToolIntegration = &declared
			replacement.Descriptor = registration.Analyzer.Descriptor().Descriptor
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

// boundToolAnalyzer keeps caller options opaque until guest negotiation.
type boundToolAnalyzer struct {
	analyzer.Analyzer
	integrator analyzer.ToolIntegrator
	options    json.RawMessage
}

func (b boundToolAnalyzer) ToolIntegration() *analyzer.ToolIntegration {
	return b.integrator.ToolIntegration()
}

func (b boundToolAnalyzer) NegotiateTool(ctx context.Context, config, configured json.RawMessage) (*analyzer.ToolNegotiation, error) {
	effective := configured
	if len(b.options) != 0 {
		effective = b.options
	}
	if len(effective) != 0 {
		if err := analyzerhost.ValidateOptions(b.Analyzer.Descriptor().OptionsSchema, effective); err != nil {
			return nil, fmt.Errorf("analyzer %q options: %w", b.Analyzer.Descriptor().Name, err)
		}
	}
	return b.integrator.NegotiateTool(ctx, config, slices.Clone(effective))
}
