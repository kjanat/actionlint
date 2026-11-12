package config_test

import (
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestShellcheckRelativeProjectPaths(t *testing.T) {
	command := testutil.RequireTool(t, "shellcheck")
	t.Setenv("GITHUB_WORKSPACE", "")
	content := []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo $VALUE
permissions: {}
`)
	for _, selection := range []string{
		"${{ gitdir }}/.shellcheckrc",
		"${{ github.workspace }}/.shellcheckrc",
		"${{ configdir }}/../.shellcheckrc",
	} {
		t.Run(selection, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			rc := fixturefiles.Write(t, root, ".shellcheckrc", "disable=SC2086\n")
			fixturefiles.Write(t, root, ".github/.shellcheckrc", "enable=all\n")
			fixturefiles.Write(t, root, ".github/actionlint.yaml", "tools: {shellcheck: {config: '"+selection+"'}}\n")
			project, err := workspace.NewProject(checking.ConfigurationRules(), ".")
			if err != nil {
				t.Fatal(err)
			}
			result, err := checking.Analyze(t.Context(), checking.Options{Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: command}})}, []workspace.Source{{Path: ".github/workflows/test.yml", Content: content, Project: project, Config: project.Config()}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 0 || !slices.Contains(result.Inputs, rc) {
				t.Fatalf("relative project selected the wrong rc: diagnostics=%+v inputs=%v", result.Diagnostics, result.Inputs)
			}
		})
	}
}
