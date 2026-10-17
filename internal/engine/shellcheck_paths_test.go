package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/internal/processconfig"
	"actionlint.kjanat.dev/internal/tools/adapters"
)

func writeShellcheckFixture(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellcheckForTest(t *testing.T) string {
	t.Helper()
	command, err := exec.LookPath("shellcheck")
	if err != nil {
		t.Skip("ShellCheck required")
	}
	t.Setenv("SHELLCHECK_OPTS", "")
	return command
}

func TestShellcheckSourcesUseAnalysisDirectoryWithoutProject(t *testing.T) {
	root := t.TempDir()
	writeShellcheckFixture(t, root, "lib.sh", "echo $VALUE\n")
	result, err := runTestAnalysis(t.Context(), Request{
		WorkingDir: root,

		Sources: []SourceUnit{{Path: "workflow.yml", Content: []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: . ./lib.sh
`)}}, Tools: (adapters.Options{ShellCheck: shellcheckForTest(t),
			ShellcheckOptions: &processconfig.Options{Arguments: []string{"--check-sourced"}}}).Registrations(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Path != "lib.sh" || result.Diagnostics[0].Code != "SC2086" {
		t.Fatalf("source was not resolved in the requested analysis directory: %+v", result.Diagnostics)
	}
}
