package expressions_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestExprDeclaredWorkflowInputSuggestion(t *testing.T) {
	for _, expression := range []string{"github_repository", "inputs.github_repository"} {
		t.Run(expression, func(t *testing.T) {
			config, err := repoconfig.Parse([]byte("tools: {shellcheck: false, ruff: false}"))
			if err != nil {
				t.Fatal(err)
			}
			source := `on:
  workflow_call:
    inputs:
      github_repository:
        type: string
        required: true
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - env:
          VALUE: ${{ ` + expression + ` }}
        run: printf '%s\n' "$VALUE"
`
			result, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir()}, []workspace.Source{{Path: "ci.yml", Content: []byte(source), Config: config}})
			if err != nil {
				t.Fatal(err)
			}
			if expression == "inputs.github_repository" {
				if len(result.Diagnostics) != 0 {
					t.Fatalf("qualified input reported: %+v", result.Diagnostics)
				}
			} else if len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Message, `did you mean "inputs.github_repository"?`) {
				t.Fatalf("declared input suggestion: %+v", result.Diagnostics)
			}
		})
	}
}
