package expressions_test

import (
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestCompositeScriptKeywordPositions(t *testing.T) {
	for _, standalone := range []bool{false, true} {
		for _, shell := range []string{"bash", "sh"} {
			for _, tc := range []struct {
				name, expression, script string
				unsafe                   bool
			}{
				{"environment keyword", "env.WORD", "if true; then\n  %s\nfi", true},
				{"constant keyword", "'fi'", "if true; then\n  %s\nfi", true},
				{"case keyword", "case(github.ref == 'x', 'fi', 'echo')", "if true; then\n  %s\nfi", true},
				{"repository owner may be keyword", "github.repository_owner", "if true; then\n  %s\nfi", true},
				{"environment argument", "env.WORD", "echo '%s'", false},
				{"constant argument", "'fi'", "echo %s", false},
				{"repository owner argument", "github.repository_owner", "echo '%s'", false},
				{"ordinary word", "'echo'", "if true; then\n  %s\nfi", false},
			} {
				t.Run(fmt.Sprintf("standalone=%t/%s/%s", standalone, shell, tc.name), func(t *testing.T) {
					root, _ := testutil.ExecutableFixture(t, "shellcheck")
					config := "tools: {shellcheck: false, ruff: false}\n"
					testutil.WriteFixture(t, root, ".github/actionlint.yaml", config)
					script := fmt.Sprintf(tc.script, "${{ "+tc.expression+" }}")
					body := "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: " + shell + "\n      env: {WORD: fi}\n      run: |\n        " + strings.ReplaceAll(script, "\n", "\n        ") + "\n"
					testutil.WriteFixture(t, root, "inner/action.yml", body)
					result := testutil.Composite(t, root, "- uses: ./inner", application.Options{})
					if standalone {
						cfg, err := repoconfig.Parse([]byte(config))
						if err != nil {
							t.Fatal(err)
						}
						result, err = application.Analyze(t.Context(), application.Options{WorkingDir: root}, []workspace.Source{{Path: "inner/action.yml", Content: []byte(body), Config: cfg}})
						if err != nil {
							t.Fatal(err)
						}
					}
					want := 0
					if tc.unsafe {
						want = 1
					}
					if len(result.Diagnostics) != want || want == 1 && (result.Diagnostics[0].Rule != "script-interpolation" || result.Diagnostics[0].Start.Line != 10 || result.Diagnostics[0].Start.Column != 15) {
						t.Fatalf("want %d keyword findings with declaring position, got %+v", want, result.Diagnostics)
					}
				})
			}
		}
	}
}
