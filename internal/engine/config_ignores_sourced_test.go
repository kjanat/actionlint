package engine

import (
	"fmt"
	"strings"
	"testing"

	processconfig "actionlint.kjanat.dev/internal/processconfig"
)

func TestReusableIgnoreSourcedFile(t *testing.T) {
	command := shellcheckForTest(t)
	for _, composite := range []bool{false, true} {
		for _, tc := range []struct {
			selector string
			want     int
		}{
			{"file: scripts/check.sh", 0},
			{"file: scripts/check.sh, step: lint", 0},
			{"file: scripts/check.sh, step: other", 1},
			{"file: scripts/other.sh", 1},
			{"file: '.github/workflows/**'", 1},
			{"file: local/action.yml", 1},
		} {
			t.Run(fmt.Sprintf("composite=%t/%s", composite, tc.selector), func(t *testing.T) {
				root, _ := executableFixture(t)
				writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\nignores: [{rule: shellcheck, "+tc.selector+"}]\n")
				writeShellcheckFixture(t, root, "scripts/check.sh", "echo $VALUE\n")
				steps := "- run: . ./scripts/check.sh\n  id: lint\n  shell: bash\n  working-directory: ."
				if composite {
					writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    "+strings.ReplaceAll(steps, "\n", "\n    ")+"\n")
					steps = "- uses: ./local"
				}
				result := compositeAnalysis(t, root, steps, AnalysisOptions{Shellcheck: command, ShellcheckOptions: &processconfig.Options{Arguments: []string{"--check-sourced"}}})
				if len(result.Diagnostics) != tc.want {
					t.Fatalf("want %d findings, got %+v", tc.want, result.Diagnostics)
				}
			})
		}
	}
}
