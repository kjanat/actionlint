package checking_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/results"
)

type registrationBoundaryTrap struct{}

type registrationBoundaryPlain struct{ checkinputs.Checker }

func (*registrationBoundaryTrap) Descriptor() checkinputs.Descriptor {
	panic("metadata called before registration validation")
}

func (*registrationBoundaryTrap) CheckDocument(context.Context, checkinputs.Input) ([]results.Diagnostic, error) {
	panic("analysis called before registration validation")
}

func (*registrationBoundaryTrap) ToolIntegration() *checkinputs.ToolIntegration {
	panic("integration called before registration validation")
}

func (*registrationBoundaryTrap) NegotiateTool(context.Context, json.RawMessage, json.RawMessage) (*checkinputs.ToolNegotiation, error) {
	panic("negotiation called before registration validation")
}

func TestApplicationRejectsNilAnalyzersBeforeMetadata(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		analyzer checkinputs.Checker
	}{
		{name: "nil interface"},
		{name: "typed nil analyzer", analyzer: (*registrationBoundaryPlain)(nil)},
		{name: "typed nil integrator", analyzer: (*registrationBoundaryTrap)(nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, endpoint := range []struct {
				name string
				run  func(checking.Options) error
			}{
				{name: "Check", run: func(options checking.Options) error {
					_, err := checking.Check(t.Context(), options, inputmodel.Request{Kind: inputmodel.Files})
					return err
				}},
				{name: "Analyze", run: func(options checking.Options) error {
					_, err := checking.Analyze(t.Context(), options, nil)
					return err
				}},
				{name: "RequiredTools", run: func(options checking.Options) error {
					_, err := checking.RequiredTools(t.Context(), options, nil)
					return err
				}},
				{name: "ConfigureToolIntegrations", run: func(options checking.Options) error {
					_, err := checking.ConfigureToolIntegrations(options)
					return err
				}},
			} {
				t.Run(endpoint.name, func(t *testing.T) {
					t.Parallel()
					options := checking.Options{Analyzers: []checkinputs.Registration{{Checker: &registrationBoundaryTrap{}}, {Checker: tc.analyzer}}}
					if err := endpoint.run(options); err == nil || !strings.Contains(err.Error(), "nil analyzer registration") {
						t.Fatalf("invalid registration was not rejected: %v", err)
					}
				})
			}
		})
	}
}
