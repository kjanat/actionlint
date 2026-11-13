package suppression_test

import (
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	processconfig "actionlint.kjanat.dev/v2/internal/integrations/process/config"
)

func TestReusableIgnoreCompositeJob(t *testing.T) {
	t.Parallel()
	command := testutil.RequireTool(t, "shellcheck")
	for _, nested := range []bool{false, true} {
		for _, tc := range []struct {
			jobs, ignores string
			want          int
		}{
			{"a", "[{rule: shellcheck, job: a, step: lint, file: scripts/check.sh}]", 0},
			{"a", "[{rule: shellcheck, job: other}]", 1},
			{"ab", "[{rule: shellcheck, job: a}]", 1},
			{"ab", "[{rule: shellcheck, job: b}]", 1},
			{"ab", "[{rule: shellcheck, job: a}, {rule: shellcheck, job: b}]", 0},
		} {
			t.Run(fmt.Sprintf("nested=%t/%s/%s", nested, tc.jobs, tc.ignores), func(t *testing.T) {
				t.Parallel()
				root, _ := testutil.ExecutableFixture(t, "shellcheck")
				config, err := checking.ConfigurationRules().Parse([]byte("tools: {shellcheck: true, ruff: false}\nignores: " + tc.ignores + "\n"))
				if err != nil {
					t.Fatal(err)
				}
				fixturefiles.Write(t, root, "scripts/check.sh", "echo $VALUE\n")
				fixturefiles.Write(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: . ./scripts/check.sh\n      id: lint\n      shell: bash\n      working-directory: .\n")
				uses := "./local"
				if nested {
					fixturefiles.Write(t, root, "outer/action.yml", "name: outer\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: ./local\n")
					uses = "./outer"
				}
				var source strings.Builder
				source.WriteString("on: push\njobs:\n")
				for _, job := range tc.jobs {
					fmt.Fprintf(&source, "  %c:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: %s\n", job, uses)
				}
				source.WriteString("permissions: {}\n")
				path := fixturefiles.Write(t, root, ".github/workflows/ci.yml", source.String())
				result, err := checking.Analyze(t.Context(), checking.Options{WorkingDir: root, Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: command, Process: &processconfig.Options{Arguments: []string{"--check-sourced"}}}})}, []workspace.Source{{Path: path, Content: []byte(source.String()), Config: config, Project: workspace.Identity(root)}})
				if err != nil {
					t.Fatal(err)
				}
				if len(result.Diagnostics) != tc.want {
					t.Fatalf("want %d findings, got %+v", tc.want, result.Diagnostics)
				}
			})
		}
	}
}
