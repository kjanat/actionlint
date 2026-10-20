package localapp

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"actionlint.kjanat.dev/internal/config"
)

func TestPluginSelectionNormalizesEachInheritedConfig(t *testing.T) {
	directory := t.TempDir()
	base := filepath.Join(directory, "base.yaml")
	path := filepath.Join(directory, "actionlint.yaml")
	if err := os.WriteFile(base, []byte("config-variables: [BASE]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("extends: [base.yaml]\nplugins: []\nlint:\n  rules:\n    correctness:\n      expression-reference:\n        level: on\n        options: {variables: [LEAF]}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	manifest, err := OpenPluginManifest(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := config.ResolveBeforePlugins(manifest.node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(resolved.Config.ConfigVariables, []string{"LEAF"}) {
		t.Fatalf("inherited legacy input overrode leaf: %#v", resolved.Config.ConfigVariables)
	}
	if !manifest.Declared || len(manifest.References) != 0 {
		t.Fatalf("empty plugin opt-out lost: %+v", manifest)
	}
}
