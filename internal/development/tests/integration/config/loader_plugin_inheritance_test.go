package config_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	configplugins "actionlint.kjanat.dev/v2/internal/input/configuration/plugins"
)

func TestPluginSelectionNormalizesEachInheritedConfig(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	base := filepath.Join(directory, "base.yaml")
	path := filepath.Join(directory, "actionlint.yaml")
	if err := os.WriteFile(base, []byte("config-variables: [BASE]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("extends: [base.yaml]\nplugins: []\nlint:\n  rules:\n    correctness:\n      expression-reference:\n        level: on\n        options: {variables: [LEAF]}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	rules := checking.ConfigurationRules()
	manifest, err := configplugins.Open(t.Context(), rules, path)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := rules.ResolveBeforePlugins(manifest.MergedDocument(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(config.VariableNames(resolved.Config.Facts()), []string{"LEAF"}) {
		t.Fatalf("inherited legacy input overrode leaf: %#v", config.VariableNames(resolved.Config.Facts()))
	}
	if !manifest.Declared || len(manifest.References) != 0 {
		t.Fatalf("empty plugin opt-out lost: %+v", manifest)
	}
}
