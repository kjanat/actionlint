package engine

import (
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"
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
      - run: echo '${{ ` + expression + ` }}'
`
			result, err := runTestAnalysis(t.Context(), Request{
				WorkingDir: t.TempDir(),
				Sources:    []SourceUnit{{Path: "ci.yml", Content: []byte(source), Config: config}},
			})
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
