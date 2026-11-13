package checking_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	workspace "actionlint.kjanat.dev/v2/internal/input/workspace"
	processconfig "actionlint.kjanat.dev/v2/internal/integrations/process/config"
	"actionlint.kjanat.dev/v2/internal/results"
)

type integratedAnalyzer struct {
	tool, contract string
	calls          int
	explicit       json.RawMessage
	unsupported    bool
	schema         json.RawMessage
}

func managedTestOptions(guest *integratedAnalyzer, options json.RawMessage) checking.Options {
	return checking.Options{Analyzers: []checkinputs.Registration{{Checker: guest, Options: options}}, Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: "shellcheck", ManagedOnly: true}})}
}

func TestManagedFallbackPlanningHonorsPluginRuleLevel(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, rules string
		want        bool
	}{
		{"plugin enables fallback", "external: {shellcheck: off}, plugins: {shellcheck: on}", true},
		{"plugin disables fallback", "external: {shellcheck: on}, plugins: {shellcheck: off}", false},
		{"global disable wins", "disable: [shellcheck], external: {shellcheck: off}, plugins: {shellcheck: on}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			configPath := filepath.Join(root, "actionlint.yml")
			if err := os.WriteFile(configPath, []byte("lint: {rules: {"+tc.rules+"}}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			guest := &integratedAnalyzer{tool: "shellcheck", contract: "kjanat:actionlint/shellcheck-config@0.1.0", unsupported: true}
			options := checking.Options{WorkingDir: root, ConfigFile: configPath, Analyzers: []checkinputs.Registration{{Checker: guest}}, Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: "shellcheck"}})}
			needed, err := checking.RequiredTools(t.Context(), options, []string{"workflow.yml"})
			if err != nil || needed["shellcheck"] != tc.want {
				t.Fatalf("native fallback required=%v, want %t; error=%v", needed, tc.want, err)
			}
		})
	}
}

func (a *integratedAnalyzer) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: a.tool, Category: "correctness", Provider: "arbitrary-fork", OptionsSchema: a.schema, RequiredInputs: []rulemeta.InputKind{rulemeta.Scripts}}
}

func (a *integratedAnalyzer) ToolIntegration() *checkinputs.ToolIntegration {
	return &checkinputs.ToolIntegration{Tool: a.tool, Contract: a.contract, ConfigurationSchema: `{}`}
}

func (a *integratedAnalyzer) NegotiateTool(_ context.Context, _ json.RawMessage, explicit json.RawMessage) (*checkinputs.ToolNegotiation, error) {
	a.explicit = explicit
	if a.unsupported {
		return nil, &checkinputs.UnsupportedToolConfigurationError{Reason: "unsupported test option"}
	}
	return &checkinputs.ToolNegotiation{Options: json.RawMessage(`{}`)}, nil
}

func TestPublicManagedOptionsStayOpaqueAndNativeFallbackStaysDisabled(t *testing.T) {
	t.Parallel()
	for _, unsupported := range []bool{false, true} {
		guest := &integratedAnalyzer{tool: "shellcheck", contract: "kjanat:actionlint/shellcheck-config@0.1.0", unsupported: unsupported}
		explicit := json.RawMessage(`{"private-option": [1, 2]}`)
		_, err := checking.Analyze(t.Context(), managedTestOptions(guest, explicit), []workspace.Source{{Path: "workflow.yml", Content: []byte(workflow)}})
		if (err != nil) != unsupported {
			t.Fatalf("unsupported=%t error=%v", unsupported, err)
		}
		if string(guest.explicit) != string(explicit) {
			t.Fatalf("opaque options changed: %s", guest.explicit)
		}
		if unsupported && guest.calls != 0 {
			t.Fatal("unsupported configuration reached analysis")
		}
	}
}

func TestPublicManagedOptionsValidateBeforeNegotiation(t *testing.T) {
	t.Parallel()
	guest := &integratedAnalyzer{tool: "shellcheck", contract: "kjanat:actionlint/shellcheck-config@0.1.0", schema: json.RawMessage(`{"type":"object","additionalProperties":false}`)}
	_, err := checking.Analyze(t.Context(), managedTestOptions(guest, json.RawMessage(`{"unknown":true}`)), []workspace.Source{{Path: "workflow.yml", Content: []byte(workflow)}})
	if err == nil || guest.explicit != nil {
		t.Fatalf("invalid options reached negotiation: %v", err)
	}
}

func TestPublicManagedRepositoryOptionsAndExplicitPrecedence(t *testing.T) {
	t.Parallel()
	for _, explicit := range []json.RawMessage{nil, json.RawMessage(`{"private-option":"caller"}`)} {
		guest := &integratedAnalyzer{tool: "shellcheck", contract: "kjanat:actionlint/shellcheck-config@0.1.0"}
		cfg, err := checking.ConfigurationRules().Parse([]byte(`lint: {rules: {plugins: {shellcheck: {level: default, options: {private-option: repository}}}}}`), "shellcheck")
		if err != nil {
			t.Fatal(err)
		}
		_, err = checking.Analyze(t.Context(), managedTestOptions(guest, explicit), []workspace.Source{{Path: "workflow.yml", Content: []byte(workflow), Config: cfg}})
		if err != nil {
			t.Fatal(err)
		}
		want := `{"private-option":"repository"}`
		if len(explicit) != 0 {
			want = string(explicit)
		}
		if string(guest.explicit) != want {
			t.Fatalf("got %s want %s", guest.explicit, want)
		}
	}
}

func (a *integratedAnalyzer) CheckDocument(_ context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	a.calls++
	return []results.Diagnostic{{Message: "managed finding", Path: input.Path, Start: input.Scripts[0].Start}}, nil
}

func TestPublicManagedAnalyzerRunsOnceAndHonorsDisable(t *testing.T) {
	t.Parallel()
	for _, disabled := range []bool{false, true} {
		guest := &integratedAnalyzer{tool: "shellcheck", contract: "kjanat:actionlint/shellcheck-config@0.1.0"}
		options := managedTestOptions(guest, nil)
		if disabled {
			options.Tools = registry.Registrations(registry.Selections{"shellcheck": {Process: &processconfig.Options{Executable: new("")}}})
		}
		result, err := checking.Analyze(t.Context(), options, []workspace.Source{{Path: "workflow.yml", Content: []byte(workflow)}})
		if err != nil {
			t.Fatal(err)
		}
		want := 1
		if disabled {
			want = 0
		}
		if guest.calls != want || len(result.Diagnostics) != want {
			t.Fatalf("disabled=%t calls=%d findings=%v", disabled, guest.calls, result.Diagnostics)
		}
	}
}

func TestPublicManagedRuleDiscoveryMatchesExecutionContracts(t *testing.T) {
	t.Parallel()
	valid := func() *integratedAnalyzer {
		return &integratedAnalyzer{tool: "shellcheck", contract: "kjanat:actionlint/shellcheck-config@0.1.0"}
	}
	options := checking.Options{
		Tools:     registry.Registrations(registry.Selections{"shellcheck": {Command: "shellcheck", ManagedOnly: true}}),
		Analyzers: []checkinputs.Registration{{Checker: valid()}},
	}
	configured, err := checking.ConfigureToolIntegrations(options)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, registration := range configured.Tools {
		descriptor := registration.Descriptor
		if descriptor.Name == "shellcheck" {
			found = descriptor.Provider == "arbitrary-fork"
		}
	}
	if !found {
		t.Fatal("managed descriptor missing")
	}
	for _, guests := range [][]checkinputs.Checker{{valid(), valid()}, {&integratedAnalyzer{tool: "unknown", contract: "unknown@0.1.0"}}, {&integratedAnalyzer{tool: "shellcheck", contract: "unknown@0.1.0"}}} {
		options.Analyzers = nil
		for _, guest := range guests {
			options.Analyzers = append(options.Analyzers, checkinputs.Registration{Checker: guest})
		}
		if _, err := checking.ConfigureToolIntegrations(options); err == nil {
			t.Fatal("invalid managed integration accepted")
		}
	}
}

func TestManagedAnalyzerCannotReplaceSelectedScriptEngine(t *testing.T) {
	t.Parallel()
	guest := &integratedAnalyzer{tool: "shellcheck", contract: "kjanat:actionlint/shellcheck-config@0.1.0"}
	options := checking.Options{Analyzers: []checkinputs.Registration{{Checker: guest}}, Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: "shellcheck"}})}
	if _, err := checking.ConfigureToolIntegrations(options, "shellcheck"); err == nil {
		t.Fatal("document analyzer replaced an already selected engine")
	}
	if len(options.Analyzers) != 1 {
		t.Fatal("failed selection mutated caller registrations")
	}
}

func TestManagedIntegrationRepeatedConfigurationRejectsNewProvider(t *testing.T) {
	t.Parallel()
	guest := &integratedAnalyzer{tool: "shellcheck", contract: "kjanat:actionlint/shellcheck-config@0.1.0"}
	options := checking.Options{Analyzers: []checkinputs.Registration{{Checker: guest}}, Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: "shellcheck"}})}
	configured, err := checking.ConfigureToolIntegrations(options)
	if err != nil {
		t.Fatal(err)
	}
	configured, err = checking.ConfigureToolIntegrations(configured)
	if err != nil {
		t.Fatalf("unchanged configuration must remain valid: %v", err)
	}
	other := &integratedAnalyzer{tool: "shellcheck", contract: "kjanat:actionlint/shellcheck-config@0.1.0"}
	configured.Analyzers = []checkinputs.Registration{{Checker: other}}
	if _, err := checking.ConfigureToolIntegrations(configured); err == nil {
		t.Fatal("new provider silently replaced existing negotiated integration")
	}
	if len(configured.Analyzers) != 1 || configured.Analyzers[0].Checker != other {
		t.Fatal("failed configuration mutated caller registrations")
	}
}

func TestPublicManagedRuleLevelsPreserveExternalPolicy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name             string
		external, plugin string
		want             int
	}{
		{"default", "", "", 1},
		{"external off with plugin options", "off", "", 0},
		{"explicit plugin on", "off", "on", 1},
		{"explicit plugin off", "on", "off", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			guest := &integratedAnalyzer{tool: "shellcheck", contract: "kjanat:actionlint/shellcheck-config@0.1.0"}
			external, plugin := tc.external, tc.plugin
			if external == "" {
				external = "default"
			}
			if plugin == "" {
				plugin = "default"
			}
			cfg, err := checking.ConfigurationRules().Parse([]byte("lint: {rules: {external: {shellcheck: "+external+"}, plugins: {shellcheck: {level: "+plugin+", options: {private-option: repository}}}}}"), "shellcheck")
			if err != nil {
				t.Fatal(err)
			}
			_, err = checking.Analyze(t.Context(), managedTestOptions(guest, nil), []workspace.Source{{Path: "workflow.yml", Content: []byte(workflow), Config: cfg}})
			if err != nil {
				t.Fatal(err)
			}
			if guest.calls != tc.want {
				t.Fatalf("calls=%d want=%d", guest.calls, tc.want)
			}
		})
	}
}
