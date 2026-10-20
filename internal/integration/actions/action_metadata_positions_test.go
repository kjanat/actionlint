package actions_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestCompositeSuggestionPositions(t *testing.T) {
	for _, tc := range []struct {
		name, declaration, token string
		fallback                 bool
	}{
		{"plain", "run: echo ${{ github_repository }}", "github_repository", false},
		{"unicode", "run: echo café ${{ github_repository }}", "github_repository", false},
		{"later-expression", "run: echo ${{ github.ref }} ${{ github_repository }}", "github_repository", false},
		{"literal", "run: |\n        echo before\n        echo ${{ github_repository }}", "github_repository", false},
		{"multiline-plain", "run: echo before\n        ${{ github_repository }}", "github_repository", false},
		{"bare-if", "run: echo ok\n      if: github_repository == 'x'", "github_repository", false},
		{"declared-input", "run: echo ${{ custom_name }}", "custom_name", false},
		{"quoted", `run: "echo ${{ github_repository }}"`, "github_repository", false},
		{"tagged-quoted", `run: !!str "echo ${{ github_repository }}"`, "github_repository", false},
		{"anchored-quoted", `run: &script "echo ${{ github_repository }}"`, "github_repository", false},
		{"tagged-anchored-folded", "run: !!str &script >\n        echo ${{ github_repository }}", "github_repository", false},
		{"escaped", `run: "echo ${{ \u0067ithub_repository }}"`, `\u0067ithub_repository`, false},
		{"single-quoted", `run: 'echo ''hello'' ${{ github_repository }}'`, "github_repository", false},
		{"folded", "run: >\n        echo ${{ github_repository }}", "github_repository", false},
		{"folded-lines", "run: >\n        echo first\n        ${{ github_repository }}", "github_repository", false},
		{"folded-condition", "run: echo ok\n      if: ${{ 'github_repository' }}", "github_repository", true},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(tc.name+ending, func(t *testing.T) {
				root, _ := testutil.ExecutableFixture(t)
				testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
				metadata := "name: inner\ndescription: test\ninputs:\n  custom_name:\n    description: custom\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      " + tc.declaration + "\n"
				testutil.WriteFixture(t, root, "inner/action.yml", strings.ReplaceAll(metadata, "\n", ending))
				result := testutil.Composite(t, root, "- uses: ./inner", application.Options{})
				count := 1
				if tc.name == "repeated" {
					count = 2
				}
				if len(result.Diagnostics) != count || !strings.Contains(result.Diagnostics[0].Message, "did you mean") {
					t.Fatalf("expected %d suggestions: %+v", count, result.Diagnostics)
				}
				var want diagmodel.Position
				for i, line := range strings.Split(metadata, "\n") {
					if tc.fallback {
						key := "run: "
						if tc.name == "folded-condition" {
							key = "if: "
						}
						if col := strings.Index(line, key); col >= 0 {
							want = diagmodel.Position{Line: i + 1, Column: col + len(key) + 1}
							break
						}
					} else if col := strings.Index(line, tc.token); col >= 0 && !strings.Contains(line, tc.token+":") {
						want = diagmodel.Position{Line: i + 1, Column: utf8.RuneCountInString(line[:col]) + 1}
						break
					}
				}
				if got := result.Diagnostics[0].Start; got != want {
					t.Fatalf("want %+v got %+v: %+v", want, got, result.Diagnostics[0])
				}
			})
		}
	}
}
