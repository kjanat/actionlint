package checking_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestLoadedConfigProvenanceReachesAnalysisInputs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	base := filepath.Join(root, "base.yml")
	leaf := filepath.Join(root, "actionlint.yml")
	if err := os.WriteFile(base, []byte("plugins: []\nfiles: {includes: []}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(leaf, []byte("extends: [base.yml]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := checking.ConfigurationRules().Read(leaf, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := checking.Analyze(t.Context(), checking.Options{WorkingDir: root},
		[]workspace.Source{{Path: "ci.yml", Content: []byte("on: push\njobs: {}\n"), Config: cfg.Config, ConfigProvenance: cfg.Provenance}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.FileCount != 0 || !slices.Contains(result.Inputs, base) || !slices.Contains(result.Inputs, leaf) {
		t.Fatalf("loaded configuration source owner lost: %+v", result)
	}
}
