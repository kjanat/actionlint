package application_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
)

type registrationBoundaryTrap struct{}

type registrationBoundaryPlain struct{ analyzer.Analyzer }

func (*registrationBoundaryTrap) Descriptor() analyzer.Descriptor {
	panic("metadata called before registration validation")
}

func (*registrationBoundaryTrap) Analyze(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error) {
	panic("analysis called before registration validation")
}

func (*registrationBoundaryTrap) ToolIntegration() *analyzer.ToolIntegration {
	panic("integration called before registration validation")
}

func (*registrationBoundaryTrap) NegotiateTool(context.Context, json.RawMessage, json.RawMessage) (*analyzer.ToolNegotiation, error) {
	panic("negotiation called before registration validation")
}

func TestApplicationRejectsNilAnalyzersBeforeMetadata(t *testing.T) {
	for _, tc := range []struct {
		name     string
		analyzer analyzer.Analyzer
	}{
		{name: "nil interface"},
		{name: "typed nil analyzer", analyzer: (*registrationBoundaryPlain)(nil)},
		{name: "typed nil integrator", analyzer: (*registrationBoundaryTrap)(nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, endpoint := range []struct {
				name string
				run  func(application.Options) error
			}{
				{name: "Check", run: func(options application.Options) error {
					_, err := application.Check(t.Context(), options, application.Input{Kind: application.Files})
					return err
				}},
				{name: "Analyze", run: func(options application.Options) error {
					_, err := application.Analyze(t.Context(), options, nil)
					return err
				}},
				{name: "RequiredTools", run: func(options application.Options) error {
					_, err := application.RequiredTools(t.Context(), options, nil)
					return err
				}},
				{name: "ConfigureToolIntegrations", run: func(options application.Options) error {
					_, err := application.ConfigureToolIntegrations(options)
					return err
				}},
			} {
				t.Run(endpoint.name, func(t *testing.T) {
					options := application.Options{Analyzers: []analyzer.Registration{{Analyzer: &registrationBoundaryTrap{}}, {Analyzer: tc.analyzer}}}
					if err := endpoint.run(options); err == nil || !strings.Contains(err.Error(), "nil analyzer registration") {
						t.Fatalf("invalid registration was not rejected: %v", err)
					}
				})
			}
		})
	}
}
