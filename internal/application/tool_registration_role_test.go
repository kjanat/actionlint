package application_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
)

type selectedToolGuest struct{}

func (selectedToolGuest) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "shellcheck", Category: "correctness", Provider: "shellcheck-rs", Maturity: "stable"}
}

func (selectedToolGuest) Analyze(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error) {
	return nil, nil
}

func (selectedToolGuest) ToolIntegration() *analyzer.ToolIntegration {
	return &analyzer.ToolIntegration{Tool: "shellcheck", Contract: "kjanat:actionlint/shellcheck-config@0.1.0"}
}

func (selectedToolGuest) NegotiateTool(context.Context, json.RawMessage, json.RawMessage) (*analyzer.ToolNegotiation, error) {
	return &analyzer.ToolNegotiation{Options: json.RawMessage(`{}`)}, nil
}

func TestToolRegistrationUsesSelectedProvider(t *testing.T) {
	for _, tc := range []struct {
		name     string
		explicit bool
	}{{name: "integrated"}, {name: "explicit process", explicit: true}} {
		t.Run(tc.name, func(t *testing.T) {
			selection := registry.Selections{"shellcheck": {Command: "shellcheck"}}
			if tc.explicit {
				selection = registry.Selections{"shellcheck": {Command: "shellcheck", Process: &processconfig.Options{}}}
			}
			options, err := application.ConfigureToolIntegrations(application.Options{Tools: registry.Registrations(selection), Analyzers: []analyzer.Registration{{Analyzer: selectedToolGuest{}}}})
			if err != nil {
				t.Fatal(err)
			}
			index := slices.IndexFunc(options.Tools, func(tool tools.Registration) bool { return tool.Descriptor.Name == "shellcheck" })
			if index < 0 {
				t.Fatal("shellcheck registration missing")
			}
			got := options.Tools[index]
			if tc.explicit {
				if got.ToolIntegration != nil || got.Descriptor.Provider == "shellcheck-rs" {
					t.Fatalf("unselected guest became provider: %+v", got.Descriptor)
				}
			} else if got.ToolIntegration == nil || got.Descriptor.Provider != "shellcheck-rs" {
				t.Fatalf("selected guest lost provider identity: %+v", got.Descriptor)
			}
		})
	}
}
