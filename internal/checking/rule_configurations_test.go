package checking

import (
	"encoding/json"
	"fmt"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/toolsettings"
)

func TestRegisteredRuleConfigurationScopes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                   string
		scope                  rule.ConfigurationScope
		actions                bool
		wantSource, wantAction string
	}{
		{"source", rule.SourceConfiguration, true, "error", "error"},
		{"composite", rule.CompositeConfiguration, true, "on", "error"},
		{"caller", rule.CallerConfiguration, false, "on", "on"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			registration := configuration.RuleRegistration{Declaration: rule.Declaration{Descriptor: rule.Descriptor{Name: "plugin-check", Category: "correctness", Maturity: "stable", Recommended: true}, Scope: tc.scope, SupportsActions: tc.actions}}
			rules, err := ConfigurationRules().WithRegisteredRules(registration)
			if err != nil {
				t.Fatal(err)
			}
			project, err := rules.ParseConfig([]byte("lint:\n  rules:\n    correctness: {plugin-check: warn}\noverrides:\n  - includes: ['inner/**']\n    lint:\n      rules:\n        correctness: {plugin-check: error}\n"))
			if err != nil {
				t.Fatal(err)
			}
			settings := newRuleConfigurations(project, t.TempDir(), configuration.Presets{}, rules, []configuration.RuleRegistration{registration})
			source, err := settings.source("inner/action.yml")
			if err != nil {
				t.Fatal(err)
			}
			action, err := settings.action("inner/action.yml")
			if err != nil {
				t.Fatal(err)
			}
			for _, result := range []struct {
				config *configuration.Config
				want   string
			}{{source, tc.wantSource}, {action, tc.wantAction}} {
				if got := result.config.RuleSet().DiagnosticLevel(result.config, "plugin-check"); string(got) != result.want {
					t.Fatalf("level = %s, want %s", got, result.want)
				}
				declaration, found := result.config.RuleSet().FindRule("plugin-check")
				if !found || declaration.Scope != tc.scope || declaration.SupportsActions != tc.actions {
					t.Fatalf("resolved declaration lost configuration scope: %+v", declaration)
				}
			}
		})
	}
}

func TestProgrammaticSourceOverridesApplyToActionMetadata(t *testing.T) {
	t.Parallel()
	registration := configuration.RuleRegistration{Declaration: rule.Declaration{Descriptor: rule.Descriptor{Name: "plugin-check", Category: "correctness", Maturity: "stable", Recommended: true}, Scope: rule.SourceConfiguration, SupportsActions: true}}
	setting, err := configuration.NewSetting("error", nil)
	if err != nil {
		t.Fatal(err)
	}
	config := &configuration.Config{Overrides: []configuration.Override{{
		Includes: []string{"inner/**"},
		Lint: &configuration.LintConfig{Rules: configuration.LintRulesConfig{
			Correctness: configuration.NewGroup("", "", map[string]configuration.Setting{"plugin-check": setting}),
		}},
	}}}
	rules, err := ConfigurationRules().WithRegisteredRules(registration)
	if err != nil {
		t.Fatal(err)
	}
	settings := newRuleConfigurations(config, t.TempDir(), configuration.Presets{}, rules, []configuration.RuleRegistration{registration})
	for _, resolve := range []func(string) (*configuration.Config, error){settings.source, settings.action} {
		selected, err := resolve("inner/action.yml")
		if err != nil {
			t.Fatal(err)
		}
		if level := selected.RuleSet().DiagnosticLevel(selected, "plugin-check"); level != "error" {
			t.Fatalf("programmatic action override was ignored: level=%s", level)
		}
	}
}

func TestRegisteredRuleRejectsUnknownConfigurationScope(t *testing.T) {
	t.Parallel()
	registration := configuration.RuleRegistration{Declaration: rule.Declaration{Descriptor: rule.Descriptor{Name: "plugin-check", Category: "correctness", Maturity: "stable"}, Scope: 255}}
	if _, err := ConfigurationRules().WithRegisteredRules(registration); err == nil {
		t.Fatal("unknown configuration scope was accepted")
	}
}

func TestExplicitPluginOptionsUseTheirJSONSchema(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		schema, options string
		valid           bool
	}{
		{`{}`, `"partial"`, true},
		{`{"type":"array"}`, `[1,2]`, true},
		{`{"type":"null"}`, `null`, true},
		{`{"type":"object"}`, `"partial"`, false},
		{`{}`, `{`, false},
	} {
		t.Run(tc.schema+"/"+tc.options, func(t *testing.T) {
			t.Parallel()
			registration := configuration.RuleRegistration{Declaration: rule.Declaration{Descriptor: rule.Descriptor{Name: "plugin-check", Category: "correctness", Maturity: "stable", OptionsSchema: json.RawMessage(tc.schema)}}, Options: json.RawMessage(tc.options)}
			_, err := ConfigurationRules().ResolveRegisteredRules(configuration.Presets{}, &configuration.Config{}, registration)
			if (err == nil) != tc.valid {
				t.Fatalf("options=%s schema=%s: %v", tc.options, tc.schema, err)
			}
		})
	}
}

func TestSourceConfigurationsProjectDeclaredAliases(t *testing.T) {
	t.Parallel()
	for _, category := range []string{"conventions", "policy"} {
		for _, name := range []string{"forbidden-suppression", "disallow-suppressions"} {
			t.Run(category+"/"+name, func(t *testing.T) {
				t.Parallel()
				project, err := ConfigurationRules().ParseConfig(fmt.Appendf(nil, "policy: {disallow-suppressions: true}\noverrides:\n  - includes: ['inner/**']\n    lint: {rules: {%s: {%s: off}}}\n", category, name))
				if err != nil {
					t.Fatal(err)
				}
				settings := newRuleConfigurations(project, t.TempDir(), configuration.Presets{}, ConfigurationRules(), nil)
				config, err := settings.source("inner/action.yml")
				if err != nil {
					t.Fatal(err)
				}
				if got := ConfigurationRules().DiagnosticLevel(config, "forbidden-suppression"); got != "off" {
					t.Fatalf("source projection dropped %s.%s: %s", category, name, got)
				}
			})
		}
	}
}

func TestDefaultRuleConfigurationsAreResolvedBeforeUse(t *testing.T) {
	t.Parallel()
	settings := newRuleConfigurations(nil, t.TempDir(), configuration.Presets{}, ConfigurationRules(), nil)
	config, err := settings.workflow("ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	if config == nil {
		t.Fatal("default rule configuration was not prepared")
	}
	for _, rule := range ConfigurationRules().Rules() {
		if _, found := config.Lint.Rules.ResolvedLevel(rule.Name); !found {
			t.Fatalf("default level for %q was not resolved", rule.Name)
		}
	}
}

func TestRuleConfigurationsSharePreparedScopeValues(t *testing.T) {
	t.Parallel()
	project, err := ConfigurationRules().ParseConfig([]byte("lint:\n  rules:\n    correctness: {action: warn, shellcheck: info}\noverrides:\n  - includes: ['inner/**']\n    lint:\n      rules:\n        correctness: {shellcheck: error}\n"))
	if err != nil {
		t.Fatal(err)
	}
	settings := newRuleConfigurations(project, t.TempDir(), configuration.Presets{}, ConfigurationRules(), nil)
	const path = "inner/action.yml"
	action, err := settings.action(path)
	if err != nil {
		t.Fatal(err)
	}
	source, err := settings.source(path)
	if err != nil {
		t.Fatal(err)
	}
	if source != settings.sources[path] || source == action {
		t.Fatal("source and action scopes did not receive separate cached configurations")
	}
	again, err := settings.action(path)
	if err != nil || again != action {
		t.Fatalf("action scope was resolved again: %p != %p, %v", again, action, err)
	}
	again, err = settings.source(path)
	if err != nil || again != source {
		t.Fatalf("source scope was resolved again: %p != %p, %v", again, source, err)
	}
	for _, config := range []*configuration.Config{action, source} {
		if got := ConfigurationRules().DiagnosticLevel(config, "shellcheck"); got != "error" {
			t.Fatalf("source override was not resolved before consumption: %s", got)
		}
	}
	if got := ConfigurationRules().DiagnosticLevel(action, "action"); got != "warn" {
		t.Fatalf("action setting = %s", got)
	}
	if got := ConfigurationRules().DiagnosticLevel(source, "action"); got == "warn" {
		t.Fatal("caller-scoped action setting leaked into the source scope")
	}
}

func TestPreparedSourceRuleRetainsToolBackendOverride(t *testing.T) {
	t.Parallel()
	registration := configuration.RuleRegistration{
		Declaration:  rule.Declaration{Descriptor: rule.Descriptor{Name: "custom-script", Category: "correctness", Maturity: "stable", Recommended: true}, Scope: rule.SourceConfiguration, SupportsActions: true},
		ToolSettings: &toolsettings.Settings{Schema: json.RawMessage(`{"type":"object","additionalProperties":false}`)},
	}
	rules, err := ConfigurationRules().WithRegisteredRules(registration)
	if err != nil {
		t.Fatal(err)
	}
	project, err := rules.ParseConfig([]byte(`tools:
  custom-script:
    backend: {wasm: {path: workflow.wasm}}
lint: {rules: {correctness: {action: warn}}}
overrides:
  - includes: ['inner/**']
    tools:
      custom-script:
        backend: {wasm: {path: action.wasm}}
`))
	if err != nil {
		t.Fatal(err)
	}
	settings := newRuleConfigurations(project, t.TempDir(), configuration.Presets{}, rules, []configuration.RuleRegistration{registration})
	entry, err := settings.workflow("ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	settings.bindEntry("ci.yml", entry)
	scope, err := settings.bindSource("inner/action.yml")
	if err != nil {
		t.Fatal(err)
	}
	prepared := scope.Rule("custom-script")
	for _, cfg := range []*configuration.Config{prepared.Config(), prepared.Policy()} {
		module, ok := cfg.Tools["custom-script"].Backend.Wasm()
		if !ok || module.Path() != "action.wasm" {
			t.Fatalf("source tool lost its backend override: module=%+v selected=%v", module, ok)
		}
		if rules.DiagnosticLevel(cfg, "action") == "warn" {
			t.Fatal("source projection retained caller-only rule settings")
		}
	}
}
