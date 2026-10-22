package tools_test

import (
	"testing"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/processconfig"
	"actionlint.kjanat.dev/internal/testutil"
	"actionlint.kjanat.dev/internal/tools/adapters"
	"actionlint.kjanat.dev/internal/workspace"
)

func TestShellcheckSourcesUseAnalysisDirectoryWithoutProject(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFixture(t, root, "lib.sh", "echo $VALUE\n")
	result, err := application.Analyze(t.Context(), application.Options{
		WorkingDir: root,

		Tools: (adapters.Options{
			ShellCheck:        testutil.RequireShellcheck(t),
			ShellcheckOptions: &processconfig.Options{Arguments: []string{"--check-sourced"}},
		}).Registrations(),
	}, []workspace.Source{{Path: "workflow.yml", Content: []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: . ./lib.sh
`)}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Path != "lib.sh" || result.Diagnostics[0].Code != "SC2086" {
		t.Fatalf("source was not resolved in the requested analysis directory: %+v", result.Diagnostics)
	}
}
