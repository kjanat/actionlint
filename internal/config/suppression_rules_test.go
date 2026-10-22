package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRegisteredRuleSelectors(t *testing.T) {
	for _, input := range []string{
		"ignores: [{rule: no-sudo, job: check}]",
		"lint: {rules: {disable: [no-sudo]}}",
		"policy: {disallow-suppressions: {rules: [no-sudo]}}",
		"lint: {rules: {policy: {disallow-suppressions: {level: on, options: {rules: [no-sudo]}}}}}",
		"overrides: [{includes: ['**'], lint: {rules: {disable: [no-sudo]}}}]",
		"overrides: [{includes: ['**'], lint: {rules: {policy: {disallow-suppressions: {level: on, options: {rules: [no-sudo]}}}}}}]",
	} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse([]byte(input)); err == nil {
				t.Fatal("standalone parsing accepted an unregistered rule")
			}
			if _, err := Parse([]byte(input), "no-sudo"); err != nil {
				t.Fatal(err)
			}
			if _, err := Parse([]byte(strings.ReplaceAll(input, "no-sudo", "typo")), "no-sudo"); err == nil {
				t.Fatal("contextual parsing accepted an unknown rule")
			}
		})
	}
}

func TestRegisteredRulesInheritanceAndOverlay(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"base.yaml":   "lint: {rules: {disable: [no-sudo]}}\n",
		"config.yaml": "extends: [base.yaml]\noverrides: [{includes: ['**'], lint: {rules: {disable: [no-sudo]}}}]\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "config.yaml")
	if _, err := Load(path); err == nil {
		t.Fatal("unregistered inherited rule accepted")
	}
	names := []string{"no-sudo"}
	source, err := Read(path, os.ReadFile, names...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseOverlay("config", []byte("ignores: [{rule: no-sudo, job: check}]")); err == nil {
		t.Fatal("unregistered overlay rule accepted")
	}
	overlay, err := ParseOverlay("config", []byte("ignores: [{rule: no-sudo, job: check}]"), "no-sudo")
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Overlaid(source.Config, source, []Overlay{overlay}, "no-sudo")
	if err != nil {
		t.Fatal(err)
	}
	effective, err := ForFile(cfg, filepath.Join(root, "ci.yml"), root)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(effective.Lint.Rules.Disable, []string{"no-sudo"}) || len(effective.Ignores) != 1 || effective.Ignores[0].Rule != "no-sudo" {
		t.Fatalf("registered rule lost through inheritance/overlay: %+v", effective)
	}
}
