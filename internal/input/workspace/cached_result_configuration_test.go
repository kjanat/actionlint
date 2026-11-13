package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/workspace"

	"actionlint.kjanat.dev/v2/internal/results/report"
)

func TestCheckResultRetainsCachedConfiguration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	config := filepath.Join(dir, "actionlint.yaml")
	workflow := filepath.Join(dir, "ci.yml")
	if err := os.WriteFile(config, []byte("config-variables: [MY_VAR]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflow, []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`), 0600); err != nil {
		t.Fatal(err)
	}
	session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: dir, ConfigFile: config, Rules: checking.ConfigurationRules()})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		analysis, err := session.Files([]string{workflow}, nil)
		if err != nil {
			t.Fatal(err)
		}
		result := report.FromAnalysis(report.Analysis{Configurations: analysis.Configurations, Inputs: analysis.Inputs})
		if len(result.Configs) != 1 || result.Configs[0].File != config || len(result.Configs[0].Origins) == 0 {
			t.Fatalf("configuration provenance lost: %+v", result.Configs)
		}
	}
}
