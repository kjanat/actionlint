package config

import (
	"reflect"
	"regexp"
	"testing"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

func TestDetachedConfigPreservesAndSeparatesMutableSettings(t *testing.T) {
	t.Parallel()
	rules, err := NewRuleSet([]rule.Declaration{{Name: "example", Category: "correctness", Maturity: "stable", Recommended: true, DefaultLevel: "warn"}})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := rules.Parse([]byte("lint: {enabled: true, rules: {correctness: {example: info}}, lifecycle: {policies: [{id: notice, description: Prepare, match: {subjects: [runners], events: [support-ends]}, notifications: [{starts: {before: 90d}, severity: warning}]}]}}\noverrides: [{includes: ['**'], lint: {enabled: false}}]\n"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = rules.ResolveRules(Presets{Strict: true}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	dynamic := []string{"one", "two"}
	settings, err := ToolConfigInline("custom", map[string]any{"items": dynamic, "limit": int(3)})
	if err != nil {
		t.Fatal(err)
	}
	settings.fromInput = true
	cfg.Tools = ToolsConfig{"custom": {Config: settings}}
	cfg.Overrides[0].Tools = &ToolsConfig{"custom": {Config: settings}}
	longest := regexp.MustCompile("a|ab")
	longest.Longest()
	cfg.Paths = map[string]PathConfig{"**": {Ignore: IgnorePatterns{longest, regexp.MustCompile("x|xy")}}}
	cfg.Policy.RequireCommitHash = new(true)
	cfg.Lint.locations = map[string]Input{"/lint/lifecycle/policies/0/id": {File: "base.toml", Reset: &Input{File: "reset.toml"}}}
	cfg.Lint.Rules.bindingSources = []bindingSource{{rules: LintRulesConfig{Disable: []string{"example"}}, inputs: map[string]Input{"/origin": {File: "base", Reset: &Input{File: "parent"}}}, prefix: []string{"lint"}}}
	lintPattern := regexp.MustCompile("y|yz")
	lintPattern.Longest()
	cfg.Lint.MessageIgnores = map[string]IgnorePatterns{"**": {lintPattern}}
	copied, err := cfg.Detached()
	if err != nil {
		t.Fatal(err)
	}
	if copied.Overrides[0].Tools == nil || !(*copied.Overrides[0].Tools)["custom"].Config.FromInput() {
		t.Fatal("override tool provenance lost")
	}
	if !copied.Tools["custom"].Config.FromInput() {
		t.Fatal("tool provenance lost")
	}
	if copied.Lint.MessageIgnores["**"][0].FindString("yz") != "yz" {
		t.Fatal("lint regexp matching mode lost")
	}
	copied.Lint.Rules.bindingSources[0].rules.Disable[0] = "changed"
	copied.Lint.Rules.bindingSources[0].inputs["/origin"].Reset.File = "changed"
	copied.Lint.Rules.bindingSources[0].prefix[0] = "changed"
	if cfg.Lint.Rules.bindingSources[0].rules.Disable[0] != "example" || cfg.Lint.Rules.bindingSources[0].inputs["/origin"].Reset.File != "parent" || cfg.Lint.Rules.bindingSources[0].prefix[0] != "lint" {
		t.Fatal("private binding source aliases original")
	}
	if copied.Paths["**"].Ignore[0].FindString("ab") != "ab" {
		t.Fatal("regexp matching mode lost")
	}
	got := copied.Tools["custom"].Config.Inline()
	items, ok := got["items"].([]string)
	if !ok || !reflect.DeepEqual(items, dynamic) {
		t.Fatalf("dynamic option type lost: %T %#v", got["items"], got["items"])
	}
	items[0] = "changed"
	if cfg.Tools["custom"].Config.Inline()["items"].([]string)[0] != "one" {
		t.Fatal("dynamic tool option aliases source")
	}
	if v, ok := got["limit"].(int); !ok || v != 3 {
		t.Fatalf("numeric type changed: %T %v", got["limit"], got["limit"])
	}
	*copied.Lint.Enabled = false
	*copied.Policy.RequireCommitHash = false
	copied.Lint.Lifecycle.Policies[0].Description = "changed"
	copied.Overrides[0].Includes[0] = "other"
	*copied.Overrides[0].Lint.Enabled = true
	copied.Paths["**"].Ignore[1].Longest()
	if !*cfg.Lint.Enabled || !*cfg.Policy.RequireCommitHash || cfg.Lint.Lifecycle.Policies[0].Description != "Prepare" || cfg.Overrides[0].Includes[0] != "**" || *cfg.Overrides[0].Lint.Enabled || cfg.Paths["**"].Ignore[0].String() != "a|ab" || cfg.Paths["**"].Ignore[1].FindString("xy") != "x" {
		t.Fatal("snapshot mutation changed source settings")
	}
	*copied.Lint.Enabled = true
	copied.Policy.RequireCommitHash = nil
	projected, err := rules.ForFile(copied, "other", "")
	if err != nil {
		t.Fatal(err)
	}
	if projected.Lint.Enabled == nil || *projected.Lint.Enabled {
		t.Fatal("snapshot lost captured override precedence")
	}
	if level, fixed := rules.FixedDiagnosticLevel(copied, "example"); level != "info" || !fixed {
		t.Fatalf("resolved rule decision lost: %q %t", level, fixed)
	}
	policies, err := copied.LifecyclePolicies()
	if err != nil || len(policies) != 1 || policies[0].Origin() != "base.toml" {
		t.Fatalf("policy origin lost: %+v %v", policies, err)
	}
	copied.Lint.locations["/lint/lifecycle/policies/0/id"].Reset.File = "changed"
	if cfg.Lint.locations["/lint/lifecycle/policies/0/id"].Reset.File != "reset.toml" {
		t.Fatal("reset origin aliases source")
	}
}

func TestDetachedConfigRejectsUnsupportedDynamicValues(t *testing.T) {
	t.Parallel()
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	for _, tc := range []struct {
		name  string
		value any
	}{{"channel", make(chan int)}, {"cycle", cyclic}, {"opaque", struct{ hidden []int }{hidden: []int{1}}}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := &Config{Tools: ToolsConfig{"custom": {Config: &ToolSettings{value: toolSettingsInline{"opaque": tc.value}}}}}
			if _, err := cfg.Detached(); err == nil {
				t.Fatal("unsupported value was silently copied")
			}
		})
	}
}

func TestLintConfigOwnsDecodedSource(t *testing.T) {
	t.Parallel()
	raw := map[string]any{"rules": map[string]any{"disable": []any{"example"}}}
	var lint LintConfig
	if err := lint.DecodeConfig(raw); err != nil {
		t.Fatal(err)
	}
	raw["rules"].(map[string]any)["disable"].([]any)[0] = "changed"
	encoded, err := lintConfigValue(lint)
	if err != nil {
		t.Fatal(err)
	}
	got := encoded.(map[string]any)["rules"].(map[string]any)["disable"].([]any)[0]
	if got != "example" {
		t.Fatalf("captured source aliases input: %v", got)
	}
}

func TestDetachedConfigPreservesPreparedPluginAndRepository(t *testing.T) {
	t.Parallel()
	rules, err := NewRuleSet(nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := rules.Parse([]byte("plugins: [publisher/plugin]\nrepository: {variables: {EXAMPLE: value}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	copied, err := cfg.Detached()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(copied.Plugins, cfg.Plugins) || !reflect.DeepEqual(copied.Repository, cfg.Repository) {
		t.Fatal("prepared plugin or repository state changed")
	}
}
