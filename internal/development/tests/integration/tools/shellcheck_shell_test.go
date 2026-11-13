package tools_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	diagmodel "actionlint.kjanat.dev/v2/internal/results"
)

func TestShellcheckResolvedDiagnostics(t *testing.T) {
	t.Parallel()
	command := testutil.RequireTool(t, "shellcheck")
	for _, tc := range []struct {
		name, container, shell string
		finding                bool
	}{
		{"host Bash", "", "", false},
		{"container sh", "alpine", "", true},
		{"container override", "alpine", "bash", false},
		{"container explicit sh", "alpine", "sh", true},
		{"empty container", "''", "", false},
		{"known container expression", "${{ 'alpine' }}", "", true},
		{"known container mapping", `${{ fromJSON('{"image":"alpine"}') }}`, "", true},
		{"empty container mapping", `${{ fromJSON('{"image":""}') }}`, "", false},
		{"unknown container", "${{ vars.IMAGE }}", "", false},
		{"override unknown container", "${{ vars.IMAGE }}", "sh", true},
		{"literal shell expression", "", "${{ 'sh' }}", true},
		{"unknown shell", "alpine", "${{ vars.SHELL }}", false},
		{"custom sh", "", "sh {0}", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
`
			if tc.container != "" {
				source += "    container: " + tc.container + "\n"
			}
			source += `    steps:
      - run: |
          [[ -n "$HOME" ]]
`
			line := strings.Count(source, "\n")
			if tc.shell != "" {
				source += "        shell: " + tc.shell + "\n"
			}
			// A following host job must not inherit container or step shell state.
			source += `  next:
    runs-on: ubuntu-latest
    steps:
      - run: '[[ -n "$HOME" ]]'
`
			request := checking.Options{WorkingDir: t.TempDir(), Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: command}})}
			var requestSources []workspace.Source
			var err error
			requestSources = []workspace.Source{{Path: "workflow.yml", InputPath: "workflow.yml", Content: []byte(source), Project: nil}}
			findings, err := testutil.Diagnostics(checking.Analyze(t.Context(), request, requestSources))
			if err != nil {
				t.Fatal(err)
			}
			var shellFindings []diagmodel.Diagnostic
			for _, finding := range findings {
				if finding.Rule == "shellcheck" {
					shellFindings = append(shellFindings, finding)
				}
			}
			if !tc.finding && len(shellFindings) == 0 {
				return
			}
			if !tc.finding || len(shellFindings) != 1 || shellFindings[0].Code != "SC3010" || shellFindings[0].Start.Line != line || shellFindings[0].Start.Column != 11 {
				t.Fatalf("expected bashism=%v at %d:11; got %v", tc.finding, line, shellFindings)
			}
		})
	}
}
