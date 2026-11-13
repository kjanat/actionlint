package suppression_test

import (
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	processconfig "actionlint.kjanat.dev/v2/internal/integrations/process/config"
)

func TestReusableIgnoreSourcedFile(t *testing.T) {
	t.Parallel()
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
				t.Parallel()
				root, _ := testutil.ExecutableFixture(t, "shellcheck")
				fixturefiles.Write(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\nignores: [{rule: shellcheck, "+tc.selector+"}]\n")
				fixturefiles.Write(t, root, "scripts/check.sh", "echo $VALUE\n")
				steps := "- run: . ./scripts/check.sh\n  id: lint\n  shell: bash\n  working-directory: ."
				if composite {
					fixturefiles.Write(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    "+strings.ReplaceAll(steps, "\n", "\n    ")+"\n")
					steps = "- uses: ./local"
				}
				result := testutil.Composite(t, root, steps, checking.Options{Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: command, Process: &processconfig.Options{Arguments: []string{"--check-sourced"}}}})})
				if len(result.Diagnostics) != tc.want {
					t.Fatalf("want %d findings, got %+v", tc.want, result.Diagnostics)
				}
			})
		}
	}
}
