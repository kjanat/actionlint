package suppression_test

import (
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
)

func TestReusableIgnoreSourcedFile(t *testing.T) {
	command := testutil.RequireTool(t, "shellcheck")
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
				root, _ := testutil.ExecutableFixture(t, "shellcheck")
				testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\nignores: [{rule: shellcheck, "+tc.selector+"}]\n")
				testutil.WriteFixture(t, root, "scripts/check.sh", "echo $VALUE\n")
				steps := "- run: . ./scripts/check.sh\n  id: lint\n  shell: bash\n  working-directory: ."
				if composite {
					testutil.WriteFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    "+strings.ReplaceAll(steps, "\n", "\n    ")+"\n")
					steps = "- uses: ./local"
				}
				result := testutil.Composite(t, root, steps, application.Options{Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: command, Process: &processconfig.Options{Arguments: []string{"--check-sourced"}}}})})
				if len(result.Diagnostics) != tc.want {
					t.Fatalf("want %d findings, got %+v", tc.want, result.Diagnostics)
				}
			})
		}
	}
}
