package config_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/analysis"
	"actionlint.kjanat.dev/v2/config"
)

func TestLoadedConfigProvenanceReachesAnalysisInputs(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "base.yml")
	leaf := filepath.Join(root, "actionlint.yml")
	if err := os.WriteFile(base, []byte("plugins: []\nfiles: {includes: []}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(leaf, []byte("extends: [base.yml]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(leaf)
	if err != nil {
		t.Fatal(err)
	}
	result, err := analysis.Analyze(t.Context(), analysis.Request{
		Sources: []analysis.Source{{Path: "ci.yml", Content: []byte("on: push\njobs: {}\n"), Config: cfg}},
		Options: analysis.Options{WorkingDir: root},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.FileCount != 0 || !slices.Contains(result.Inputs, base) || !slices.Contains(result.Inputs, leaf) {
		t.Fatalf("loaded configuration source owner lost: %+v", result)
	}
}
