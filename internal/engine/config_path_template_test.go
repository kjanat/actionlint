package engine

import (
	"slices"
	"testing"

	"actionlint.kjanat.dev/internal/tools/adapters"
	"actionlint.kjanat.dev/internal/workspace"
)

func TestShellcheckRelativeProjectPaths(t *testing.T) {
	command := shellcheckForTest(t)
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("GITHUB_WORKSPACE", "")
	rc := writeShellcheckFixture(t, root, ".shellcheckrc", "disable=SC2086\n")
	writeShellcheckFixture(t, root, ".github/.shellcheckrc", "enable=all\n")
	content := []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo $VALUE
`)
	for _, selection := range []string{
		"${{ gitdir }}/.shellcheckrc",
		"${{ github.workspace }}/.shellcheckrc",
		"${{ configdir }}/../.shellcheckrc",
	} {
		t.Run(selection, func(t *testing.T) {
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: {config: '"+selection+"'}}\n")
			project, err := workspace.NewProject(".")
			if err != nil {
				t.Fatal(err)
			}
			result, err := runTestAnalysis(t.Context(), Request{

				Sources: []SourceUnit{{Path: ".github/workflows/test.yml", Content: content, Project: project, Config: project.Config()}}, Tools: (adapters.Options{ShellCheck: command}).Registrations(),
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 0 || !slices.Contains(result.Inputs, rc) {
				t.Fatalf("relative project selected the wrong rc: diagnostics=%+v inputs=%v", result.Diagnostics, result.Inputs)
			}
		})
	}
}
