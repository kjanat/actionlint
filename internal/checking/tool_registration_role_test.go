package checking_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/contract"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/registry"
	"actionlint.kjanat.dev/v2/internal/integrations/hostprocess"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/results"
)

type selectedToolGuest struct{}

func (selectedToolGuest) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "shellcheck", Category: "correctness", Provider: "shellcheck-rs", Maturity: "stable"}
}

func (selectedToolGuest) CheckDocument(context.Context, checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
	return nil, nil
}

func (selectedToolGuest) ToolIntegration() *checkinputs.ToolIntegration {
	return &checkinputs.ToolIntegration{Tool: "shellcheck", Contract: "kjanat:actionlint/shellcheck-config@0.1.0"}
}

func (selectedToolGuest) NegotiateTool(context.Context, json.RawMessage, json.RawMessage) (*checkinputs.ToolNegotiation, error) {
	return &checkinputs.ToolNegotiation{Options: json.RawMessage(`{}`)}, nil
}

func TestToolRegistrationUsesSelectedProvider(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		explicit bool
	}{{name: "integrated"}, {name: "explicit process", explicit: true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			selection := registry.ToolSelections{"shellcheck": {Command: "shellcheck"}}
			if tc.explicit {
				selection = registry.ToolSelections{"shellcheck": {Command: "shellcheck", Process: &hostprocess.CommandOptions{}}}
			}
			options, err := checking.ConfigureToolIntegrations(checking.Options{Tools: registry.Registrations(selection), Analyzers: []checkinputs.Registration{{Checker: selectedToolGuest{}}}})
			if err != nil {
				t.Fatal(err)
			}
			index := slices.IndexFunc(options.Tools, func(tool contract.Registration) bool { return tool.Name == "shellcheck" })
			if index < 0 {
				t.Fatal("shellcheck registration missing")
			}
			got := options.Tools[index]
			if tc.explicit {
				if got.ToolIntegration != nil || got.Provider == "shellcheck-rs" {
					t.Fatalf("unselected guest became provider: %+v", got.Descriptor)
				}
			} else if got.ToolIntegration == nil || got.Provider != "shellcheck-rs" {
				t.Fatalf("selected guest lost provider identity: %+v", got.Descriptor)
			}
		})
	}
}
