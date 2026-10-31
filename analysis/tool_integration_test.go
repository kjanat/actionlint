package analysis_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/analysis"
	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/config"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
)

type integratedAnalyzer struct {
	tool, contract string
	calls          int
	explicit       json.RawMessage
	unsupported    bool
	schema         json.RawMessage
}

func TestManagedFallbackPlanningHonorsPluginRuleLevel(t *testing.T) {
	for _, tc := range []struct {
		name, rules string
		want        bool
	}{
		{"plugin enables fallback", "external: {shellcheck: off}, plugins: {shellcheck: on}", true},
		{"plugin disables fallback", "external: {shellcheck: on}, plugins: {shellcheck: off}", false},
		{"global disable wins", "disable: [shellcheck], external: {shellcheck: off}, plugins: {shellcheck: on}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			configPath := filepath.Join(root, "actionlint.yml")
			if err := os.WriteFile(configPath, []byte("lint: {rules: {"+tc.rules+"}}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			guest := &integratedAnalyzer{tool: "shellcheck", contract: "kjanat:actionlint/shellcheck-config@0.1.0", unsupported: true}
			options := application.Options{WorkingDir: root, ConfigFile: configPath, Analyzers: []analyzer.Registration{{Analyzer: guest}}, Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: "shellcheck"}})}
			needed, err := application.RequiredTools(t.Context(), options, []string{"workflow.yml"})
			if err != nil || needed["shellcheck"] != tc.want {
				t.Fatalf("native fallback required=%v, want %t; error=%v", needed, tc.want, err)
			}
		})
	}
}

func (a *integratedAnalyzer) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: a.tool, Provider: "arbitrary-fork", OptionsSchema: a.schema, Requires: []analyzer.Fact{analyzer.Scripts}}
}

func (a *integratedAnalyzer) ToolIntegration() *analyzer.ToolIntegration {
	return &analyzer.ToolIntegration{Tool: a.tool, Contract: a.contract, ConfigurationSchema: `{}`}
}

func (a *integratedAnalyzer) NegotiateTool(_ context.Context, _ json.RawMessage, explicit json.RawMessage) (*analyzer.ToolNegotiation, error) {
	a.explicit = explicit
	if a.unsupported {
		return nil, &analyzer.UnsupportedToolConfigurationError{Reason: "unsupported test option"}
	}
	return &analyzer.ToolNegotiation{Options: json.RawMessage(`{}`)}, nil
}

func TestPublicManagedOptionsStayOpaqueAndNativeFallbackStaysDisabled(t *testing.T) {
	for _, unsupported := range []bool{false, true} {
		guest := &integratedAnalyzer{tool: "shellcheck", contract: "kjanat:actionlint/shellcheck-config@0.1.0", unsupported: unsupported}
		explicit := json.RawMessage(`{"private-option": [1, 2]}`)
		_, err := analysis.Analyze(t.Context(), analysis.Request{Options: analysis.Options{Analyzers: []analyzer.Registration{{Analyzer: guest, Options: explicit}}}, Sources: []analysis.Source{{Path: "workflow.yml", Content: []byte(workflow)}}})
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
	guest := &integratedAnalyzer{tool: "shellcheck", contract: "kjanat:actionlint/shellcheck-config@0.1.0", schema: json.RawMessage(`{"type":"object","additionalProperties":false}`)}
	_, err := analysis.Analyze(t.Context(), analysis.Request{Options: analysis.Options{Analyzers: []analyzer.Registration{{Analyzer: guest, Options: json.RawMessage(`{"unknown":true}`)}}}, Sources: []analysis.Source{{Path: "workflow.yml", Content: []byte(workflow)}}})
	if err == nil || guest.explicit != nil {
		t.Fatalf("invalid options reached negotiation: %v", err)
	}
}

func TestPublicManagedRepositoryOptionsAndExplicitPrecedence(t *testing.T) {
	for _, explicit := range []json.RawMessage{nil, json.RawMessage(`{"private-option":"caller"}`)} {
		guest := &integratedAnalyzer{tool: "shellcheck", contract: "kjanat:actionlint/shellcheck-config@0.1.0"}
		cfg, err := config.Parse([]byte(`lint: {rules: {plugins: {shellcheck: {level: default, options: {private-option: repository}}}}}`), "shellcheck")
		if err != nil {
			t.Fatal(err)
		}
		_, err = analysis.Analyze(t.Context(), analysis.Request{Options: analysis.Options{Analyzers: []analyzer.Registration{{Analyzer: guest, Options: explicit}}}, Sources: []analysis.Source{{Path: "workflow.yml", Content: []byte(workflow), Config: cfg}}})
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

func (a *integratedAnalyzer) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	a.calls++
	return []diagnostic.Diagnostic{{Message: "managed finding", Path: input.Path, Start: input.Scripts[0].Start}}, nil
}

func TestPublicManagedAnalyzerRunsOnceAndHonorsDisable(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		guest := &integratedAnalyzer{tool: "shellcheck", contract: "kjanat:actionlint/shellcheck-config@0.1.0"}
		options := analysis.Options{Analyzers: []analyzer.Registration{{Analyzer: guest}}}
		if disabled {
			options.Tools = map[string]analysis.Tool{"shellcheck": {Process: &analysis.Process{}}}
		}
		result, err := analysis.Analyze(t.Context(), analysis.Request{Options: options, Sources: []analysis.Source{{Path: "workflow.yml", Content: []byte(workflow)}}})
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
	valid := func() *integratedAnalyzer {
		return &integratedAnalyzer{tool: "shellcheck", contract: "kjanat:actionlint/shellcheck-config@0.1.0"}
	}
	rules, err := analysis.Rules(valid())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, descriptor := range rules {
		if descriptor.Name == "shellcheck" {
			found = descriptor.Provider == "arbitrary-fork"
		}
	}
	if !found {
		t.Fatal("managed descriptor missing")
	}
	for _, guests := range [][]analyzer.Analyzer{{valid(), valid()}, {&integratedAnalyzer{tool: "unknown", contract: "unknown@0.1.0"}}, {&integratedAnalyzer{tool: "shellcheck", contract: "unknown@0.1.0"}}} {
		if _, err := analysis.Rules(guests...); err == nil {
			t.Fatal("invalid managed integration accepted")
		}
	}
}

func TestManagedAnalyzerCannotReplaceSelectedScriptEngine(t *testing.T) {
	guest := &integratedAnalyzer{tool: "shellcheck", contract: "kjanat:actionlint/shellcheck-config@0.1.0"}
	options := application.Options{Analyzers: []analyzer.Registration{{Analyzer: guest}}, Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: "shellcheck"}})}
	if _, err := application.ConfigureToolIntegrations(options, "shellcheck"); err == nil {
		t.Fatal("document analyzer replaced an already selected engine")
	}
	if len(options.Analyzers) != 1 {
		t.Fatal("failed selection mutated caller registrations")
	}
}

func TestManagedIntegrationRepeatedConfigurationRejectsNewProvider(t *testing.T) {
	guest := &integratedAnalyzer{tool: "shellcheck", contract: "kjanat:actionlint/shellcheck-config@0.1.0"}
	options := application.Options{Analyzers: []analyzer.Registration{{Analyzer: guest}}, Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: "shellcheck"}})}
	configured, err := application.ConfigureToolIntegrations(options)
	if err != nil {
		t.Fatal(err)
	}
	configured, err = application.ConfigureToolIntegrations(configured)
	if err != nil {
		t.Fatalf("unchanged configuration must remain valid: %v", err)
	}
	other := &integratedAnalyzer{tool: "shellcheck", contract: "kjanat:actionlint/shellcheck-config@0.1.0"}
	configured.Analyzers = []analyzer.Registration{{Analyzer: other}}
	if _, err := application.ConfigureToolIntegrations(configured); err == nil {
		t.Fatal("new provider silently replaced existing negotiated integration")
	}
	if len(configured.Analyzers) != 1 || configured.Analyzers[0].Analyzer != other {
		t.Fatal("failed configuration mutated caller registrations")
	}
}

func TestPublicManagedRuleLevelsPreserveExternalPolicy(t *testing.T) {
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
			guest := &integratedAnalyzer{tool: "shellcheck", contract: "kjanat:actionlint/shellcheck-config@0.1.0"}
			external, plugin := tc.external, tc.plugin
			if external == "" {
				external = "default"
			}
			if plugin == "" {
				plugin = "default"
			}
			cfg, err := config.Parse([]byte("lint: {rules: {external: {shellcheck: "+external+"}, plugins: {shellcheck: {level: "+plugin+", options: {private-option: repository}}}}}"), "shellcheck")
			if err != nil {
				t.Fatal(err)
			}
			_, err = analysis.Analyze(t.Context(), analysis.Request{Options: analysis.Options{Analyzers: []analyzer.Registration{{Analyzer: guest}}}, Sources: []analysis.Source{{Path: "workflow.yml", Content: []byte(workflow), Config: cfg}}})
			if err != nil {
				t.Fatal(err)
			}
			if guest.calls != tc.want {
				t.Fatalf("calls=%d want=%d", guest.calls, tc.want)
			}
		})
	}
}
