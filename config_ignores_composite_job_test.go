package actionlint

import (
	"fmt"
	"strings"
	"testing"
)

func TestReusableIgnoreCompositeJob(t *testing.T) {
	command := shellcheckForTest(t)
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
				root, _ := executableFixture(t)
				config, err := ParseConfig([]byte("tools: {shellcheck: true, ruff: false}\nignores: " + tc.ignores + "\n"))
				if err != nil {
					t.Fatal(err)
				}
				writeShellcheckFixture(t, root, "scripts/check.sh", "echo $VALUE\n")
				writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: . ./scripts/check.sh\n      id: lint\n      shell: bash\n      working-directory: .\n")
				uses := "./local"
				if nested {
					writeShellcheckFixture(t, root, "outer/action.yml", "name: outer\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: ./local\n")
					uses = "./outer"
				}
				var source strings.Builder
				source.WriteString("on: push\njobs:\n")
				for _, job := range tc.jobs {
					fmt.Fprintf(&source, "  %c:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: %s\n", job, uses)
				}
				path := writeShellcheckFixture(t, root, ".github/workflows/ci.yml", source.String())
				result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: root, Sources: []SourceUnit{{Path: path, Content: []byte(source.String()), Config: config, Project: &Project{root: root}}}, ShellCheck: command, ShellcheckOptions: &ExternalCommandOptions{Arguments: []string{"--check-sourced"}}})
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
