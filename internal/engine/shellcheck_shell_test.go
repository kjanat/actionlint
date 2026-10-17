package engine

import (
	"os/exec"
	"strings"
	"testing"

	findingmodel "actionlint.kjanat.dev/internal/finding"
	"actionlint.kjanat.dev/internal/tools/adapters"
)

func TestShellcheckResolvedDiagnostics(t *testing.T) {
	command, err := exec.LookPath("shellcheck")
	if err != nil {
		t.Skip("ShellCheck required")
	}
	t.Setenv("SHELLCHECK_OPTS", "")
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
			request := Request{WorkingDir: t.TempDir(), Tools: (adapters.Options{ShellCheck: command}).Registrations()}
			var err error

			request.Sources = []SourceUnit{{Path: "workflow.yml", InputPath: "workflow.yml", Content: []byte(source), Project: nil}}
			findings, err := testFindings(runTestAnalysis(t.Context(), request))
			if err != nil {
				t.Fatal(err)
			}
			var shellFindings []*findingmodel.Finding
			for _, finding := range findings {
				if finding.Rule == "shellcheck" {
					shellFindings = append(shellFindings, finding)
				}
			}
			if !tc.finding && len(shellFindings) == 0 {
				return
			}
			if !tc.finding || len(shellFindings) != 1 || !strings.Contains(shellFindings[0].Message, "SC3010") || shellFindings[0].Start.Line != line || shellFindings[0].Start.Column != 11 {
				t.Fatalf("expected bashism=%v at %d:11; got %v", tc.finding, line, shellFindings)
			}
		})
	}
}
