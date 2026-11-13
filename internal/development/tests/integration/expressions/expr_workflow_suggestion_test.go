package expressions_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestExprDeclaredWorkflowInputSuggestion(t *testing.T) {
	t.Parallel()
	for _, expression := range []string{"github_repository", "inputs.github_repository"} {
		t.Run(expression, func(t *testing.T) {
			t.Parallel()
			config, err := checking.ConfigurationRules().Parse([]byte("tools: {shellcheck: false, ruff: false}"))
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
` + "\npermissions: {}\n"
			result, err := checking.Analyze(t.Context(), checking.Options{WorkingDir: t.TempDir()}, []workspace.Source{{Path: "ci.yml", Content: []byte(source), Config: config}})
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
