package config_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/rule"
)

func resolveRules(t *testing.T, presets repoconfig.Presets, config *repoconfig.Config, descriptors ...rule.Descriptor) *repoconfig.Config {
	t.Helper()
	resolved, err := repoconfig.ResolveRules(presets, config, descriptors...)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestCategoryBindingProgrammaticAliases(t *testing.T) {
	setting, err := repoconfig.NewSetting("warn", map[string]int{"max-minutes": 12})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &repoconfig.Config{}
	cfg.Lint.Rules.Policy = repoconfig.NewGroup("", "", map[string]repoconfig.Setting{"require-job-timeout": setting})
	cfg.Lint.Rules.Conventions = repoconfig.NewGroup("", "error", nil)
	resolved := resolveRules(t, repoconfig.Presets{Strict: true}, cfg)
	maximum, known := resolved.RequiresJobTimeout().MaxMinutes()
	if !known || maximum != 12 || repoconfig.DiagnosticLevel(resolved, "job-timeout") != "warn" || repoconfig.DiagnosticLevel(resolved, "require-job-timeout") != "warn" {
		t.Fatal("canonical category baseline replaced explicit legacy alias")
	}
	cfg.Lint.Rules.Policy = repoconfig.NewGroup("", "", map[string]repoconfig.Setting{"require-job-timeout": setting, "job-timeout": setting})
	if _, err := repoconfig.ResolveRules(repoconfig.Presets{}, cfg); err == nil || !strings.Contains(err.Error(), "duplicate rule aliases") {
		t.Fatalf("duplicate aliases accepted: %v", err)
	}
}

func TestCategoryBindingValidatesHiddenInheritedRules(t *testing.T) {
	read := func(path string) ([]byte, error) {
		if filepath.Base(path) == "base.yml" {
			return []byte("lint: {rules: {correctness: {community-check: on}}}"), nil
		}
		return []byte("extends: [base.yml]\nlint: {rules: {correctness: null}}"), nil
	}
	loaded, err := repoconfig.Read(filepath.Join(t.TempDir(), "leaf.yml"), read)
	if err != nil {
		t.Fatal(err)
	}
	descriptor := rule.Descriptor{Name: "community-check", Category: "security", Maturity: "stable"}
	if _, err := repoconfig.ResolveRules(repoconfig.Presets{}, loaded.Config, descriptor); err == nil || !strings.Contains(err.Error(), "base.yml") || !strings.Contains(err.Error(), "lint.rules.security") {
		t.Fatalf("hidden inherited category mismatch accepted: %v", err)
	}
}

func TestCategoryBindingPluginOptions(t *testing.T) {
	descriptor := rule.Descriptor{
		Name: "community-check", Category: "security", Provider: "community", Maturity: "stable",
		OptionsSchema: json.RawMessage(`{"type":"object","properties":{"count":{"type":"integer","minimum":1}},"additionalProperties":false}`),
	}
	cfg, err := repoconfig.Parse([]byte(`lint: {rules: {security: {level: warn, community-check: {level: error, options: {count: 3}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	resolved := resolveRules(t, repoconfig.Presets{}, cfg, descriptor)
	if got := repoconfig.DiagnosticLevel(resolved, descriptor.Name); got != "error" {
		t.Fatalf("level: %s", got)
	}
	options, err := resolved.Lint.Rules.RuleOptions(descriptor.Name)
	if err != nil || options["count"] != 3 {
		t.Fatalf("options: %v, %v", options, err)
	}
	options["count"] = 10
	again, err := resolved.Lint.Rules.RuleOptions(descriptor.Name)
	if err != nil || again["count"] != 3 {
		t.Fatalf("options shared mutable state: %v, %v", again, err)
	}
	if _, bound := cfg.Lint.Rules.ResolvedLevel(descriptor.Name); bound {
		t.Fatal("resolution mutated input")
	}
}

func TestCategoryBindingFailures(t *testing.T) {
	valid := rule.Descriptor{Name: "community-check", Category: "security", Provider: "community", Maturity: "stable"}
	for _, tc := range []struct {
		name        string
		rules       string
		descriptors []rule.Descriptor
		want        string
	}{
		{"unknown rule", `security: {community-check: on}`, nil, `unknown rule "community-check"`},
		{"category mismatch", `correctness: {community-check: on}`, []rule.Descriptor{valid}, "lint.rules.security"},
		{"duplicate", `{}`, []rule.Descriptor{valid, valid}, "duplicate registered rule"},
		{"empty category", `{}`, []rule.Descriptor{{Name: "community-check"}}, "invalid category"},
		{"provider as category", `{}`, []rule.Descriptor{{Name: "community-check", Category: "plugins"}}, "invalid category"},
		{"maturity as category", `{}`, []rule.Descriptor{{Name: "community-check", Category: "nursery"}}, "invalid category"},
		{"invalid maturity", `{}`, []rule.Descriptor{{Name: "community-check", Category: "security", Maturity: "beta"}}, "invalid maturity"},
		{"old maturity spelling", `{}`, []rule.Descriptor{{Name: "shellcheck", Category: "correctness", Maturity: "experimental"}}, "invalid maturity"},
		{"builtin collision", `{}`, []rule.Descriptor{{Name: "expression", Category: "correctness", Provider: "community"}}, "conflicts"},
		{"bad schema", `{}`, []rule.Descriptor{{Name: "community-check", Category: "security", OptionsSchema: json.RawMessage(`{`)}}, "options schema"},
		{"bad options", `security: {community-check: {level: on, options: {count: wrong}}}`, []rule.Descriptor{{Name: "community-check", Category: "security", OptionsSchema: json.RawMessage(`{"type":"object","properties":{"count":{"type":"integer"}}}`)}}, `options for rule "community-check"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rules := tc.rules
			if rules != "{}" {
				rules = "{" + rules + "}"
			}
			cfg, err := repoconfig.Parse([]byte("lint: {rules: " + rules + "}"))
			if err != nil {
				t.Fatalf("structural decode rejected deferred binding: %v", err)
			}
			if _, err := repoconfig.ResolveRules(repoconfig.Presets{}, cfg, tc.descriptors...); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestCategoryBindingNurserySelection(t *testing.T) {
	for _, maturity := range []string{rule.NurseryMaturity} {
		descriptor := rule.Descriptor{Name: "community-check", Category: "correctness", Provider: "community", Maturity: maturity, Recommended: true}
		for _, tc := range []struct {
			rules   string
			presets repoconfig.Presets
			want    repoconfig.Level
		}{
			{"{}", repoconfig.Presets{}, "off"},
			{"{preset: recommended}", repoconfig.Presets{}, "off"},
			{"{preset: all}", repoconfig.Presets{}, "off"},
			{"{correctness: all}", repoconfig.Presets{}, "off"},
			{"{correctness: warn}", repoconfig.Presets{}, "off"},
			{"{nursery: all}", repoconfig.Presets{}, "on"},
			{"{nursery: warn}", repoconfig.Presets{}, "warn"},
			{"{}", repoconfig.Presets{Strict: true}, "off"},
			{"{}", repoconfig.Presets{Experimental: new(true)}, "on"},
			{"{correctness: {community-check: warn}}", repoconfig.Presets{}, "warn"},
			{"{correctness: {community-check: off}}", repoconfig.Presets{Experimental: new(true)}, "off"},
			{"{correctness: {community-check: warn}}", repoconfig.Presets{Experimental: new(false)}, "off"},
		} {
			t.Run(maturity+"/"+tc.rules+"/"+string(tc.want), func(t *testing.T) {
				cfg, err := repoconfig.Parse([]byte("lint: {rules: " + tc.rules + "}"))
				if err != nil {
					t.Fatal(err)
				}
				resolved := resolveRules(t, tc.presets, cfg, descriptor)
				if got := repoconfig.DiagnosticLevel(resolved, descriptor.Name); got != tc.want {
					t.Fatalf("got %s, want %s", got, tc.want)
				}
			})
		}
	}
}
