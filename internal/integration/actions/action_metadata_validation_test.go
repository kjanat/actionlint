package actions_test

import (
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/application"
	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/testutil"
	"actionlint.kjanat.dev/internal/workspace"
)

func TestMetadataFirstVisibleInvocation(t *testing.T) {
	for _, metadata := range []struct {
		name, source string
		foreign      bool
	}{
		{"caller", "name: inner\nruns: {using: node24, main: index.js}\n", false},
		{"metadata", "name: inner\ndescription: test\ninputs:\n  token:\n    description: token\n    default: ${{ secrets.TOKEN }}\nruns: {using: node24, main: index.js}\n", true},
	} {
		t.Run(metadata.name, func(t *testing.T) {
			root, _ := testutil.ExecutableFixture(t)
			testutil.WriteFixture(t, root, "inner/action.yml", metadata.source)
			testutil.WriteFixture(t, root, "inner/index.js", "")
			for _, tc := range []struct {
				name, ignores string
				first         int
			}{
				{"none", "[]", 0},
				{"unmatched job", "[{rule: action, job: missing}]", 0},
				{"unmatched file", "[{rule: action, file: missing.yml}]", 0},
				{"metadata file", "[{rule: action, file: inner/action.yml}]", 0},
				{"other rule", "[{rule: shellcheck, job: a}]", 0},
				{"first", "[{rule: action, job: c}]", 1},
				{"second", "[{rule: action, job: b}]", 0},
				{"first two", "[{rule: action, job: c}, {rule: action, job: b}]", 2},
				{"all", "[{rule: action, job: a}, {rule: action, job: b}, {rule: action, job: c}]", -1},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if tc.name == "metadata file" && metadata.foreign {
						tc.first = -1
					}
					config, err := repoconfig.Parse([]byte("tools: {shellcheck: false, ruff: false}\nignores: " + tc.ignores))
					if err != nil {
						t.Fatal(err)
					}
					var source strings.Builder
					source.WriteString("on: push\njobs:\n")
					for _, job := range []string{"c", "b", "a"} {
						source.WriteString("  ")
						source.WriteString(job)
						source.WriteString(":\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./inner\n")
					}
					result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root}, []workspace.Source{{Path: filepath.Join(root, ".github/workflows/ci.yml"), Content: []byte(source.String()), Config: config, Project: workspace.Identity(root)}})
					if err != nil {
						t.Fatal(err)
					}
					want := 1
					if tc.first < 0 {
						want = 0
					}
					if len(result.Diagnostics) != want {
						t.Fatalf("findings: %+v", result.Diagnostics)
					}
					if want == 0 {
						return
					}
					d := result.Diagnostics[0]
					if metadata.foreign {
						if d.Path != filepath.Join("inner", "action.yml") {
							t.Fatalf("lost metadata path: %+v", d)
						}
					} else if d.Start.Line != 6+tc.first*4 {
						t.Fatalf("wrong first visible caller: %+v", d)
					}
				})
			}
		})
	}
}
