package config_test

import (
	"slices"
	"testing"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/testutil"
	"actionlint.kjanat.dev/internal/tools/adapters"
	"actionlint.kjanat.dev/internal/workspace"
)

func TestShellcheckRelativeProjectPaths(t *testing.T) {
	command := testutil.RequireShellcheck(t)
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("GITHUB_WORKSPACE", "")
	rc := testutil.WriteFixture(t, root, ".shellcheckrc", "disable=SC2086\n")
	testutil.WriteFixture(t, root, ".github/.shellcheckrc", "enable=all\n")
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
			testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: {config: '"+selection+"'}}\n")
			project, err := workspace.NewProject(".")
			if err != nil {
				t.Fatal(err)
			}
			result, err := application.Analyze(t.Context(), application.Options{Tools: (adapters.Options{ShellCheck: command}).Registrations()}, []workspace.Source{{Path: ".github/workflows/test.yml", Content: content, Project: project, Config: project.Config()}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 0 || !slices.Contains(result.Inputs, rc) {
				t.Fatalf("relative project selected the wrong rc: diagnostics=%+v inputs=%v", result.Diagnostics, result.Inputs)
			}
		})
	}
}
