package suppression_test

import (
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestReusableIgnoreCompositeJob(t *testing.T) {
	command := testutil.RequireShellcheck(t)
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
				root, _ := testutil.ExecutableFixture(t)
				config, err := repoconfig.Parse([]byte("tools: {shellcheck: true, ruff: false}\nignores: " + tc.ignores + "\n"))
				if err != nil {
					t.Fatal(err)
				}
				testutil.WriteFixture(t, root, "scripts/check.sh", "echo $VALUE\n")
				testutil.WriteFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: . ./scripts/check.sh\n      id: lint\n      shell: bash\n      working-directory: .\n")
				uses := "./local"
				if nested {
					testutil.WriteFixture(t, root, "outer/action.yml", "name: outer\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: ./local\n")
					uses = "./outer"
				}
				var source strings.Builder
				source.WriteString("on: push\njobs:\n")
				for _, job := range tc.jobs {
					fmt.Fprintf(&source, "  %c:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: %s\n", job, uses)
				}
				path := testutil.WriteFixture(t, root, ".github/workflows/ci.yml", source.String())
				result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root, Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: command, Process: &processconfig.Options{Arguments: []string{"--check-sourced"}}}})}, []workspace.Source{{Path: path, Content: []byte(source.String()), Config: config, Project: workspace.Identity(root)}})
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
