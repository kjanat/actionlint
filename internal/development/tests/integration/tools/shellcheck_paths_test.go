package tools_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	processconfig "actionlint.kjanat.dev/v2/internal/integrations/process/config"
)

func TestShellcheckSourcesUseAnalysisDirectoryWithoutProject(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fixturefiles.Write(t, root, "lib.sh", "echo $VALUE\n")
	result, err := checking.Analyze(t.Context(), checking.Options{
		WorkingDir: root,

		Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: testutil.RequireTool(t, "shellcheck"), Process: &processconfig.Options{Arguments: []string{"--check-sourced"}}}}),
	}, []workspace.Source{{Path: "workflow.yml", Content: []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: . ./lib.sh
permissions: {}
`)}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Path != "lib.sh" || result.Diagnostics[0].Code != "SC2086" {
		t.Fatalf("source was not resolved in the requested analysis directory: %+v", result.Diagnostics)
	}
}
