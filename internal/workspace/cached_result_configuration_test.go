package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/render"
	"actionlint.kjanat.dev/v2/internal/report"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestCheckResultRetainsCachedConfiguration(t *testing.T) {
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
	session, err := workspace.New(workspace.Options{WorkingDir: dir, ConfigFile: config})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		analysis, err := session.Files([]string{workflow}, nil)
		if err != nil {
			t.Fatal(err)
		}
		result := render.Prepare(report.Analysis{Configurations: analysis.Configurations, Inputs: analysis.Inputs}).CheckResult()
		if len(result.Configs) != 1 || result.Configs[0].File != config || len(result.Configs[0].Origins) == 0 {
			t.Fatalf("configuration provenance lost: %+v", result.Configs)
		}
	}
}
