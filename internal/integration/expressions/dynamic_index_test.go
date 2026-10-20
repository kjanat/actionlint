package expressions_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestDynamicIndexJobConditionDiagnostics(t *testing.T) {
	for _, condition := range []string{"a[matrix.k] == 'x'", "${{ a[matrix.k] == 'x' }}"} {
		t.Run(condition, func(t *testing.T) {
			source := `on: push
jobs:
  b:
    if: ` + condition + `
    runs-on: ubuntu-latest
    strategy:
      matrix:
        k: [status]
        name: [TOKEN]
        env: [DEV]
    env:
      TOKEN: ${{ secrets[matrix.name] }}
      V: ${{ vars[format('X_{0}', matrix.env)] }}
    steps:
      - run: echo indexes
`
			result, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir()}, []workspace.Source{{Path: "indexes.yaml", Content: []byte(source)}})
			if err != nil {
				t.Fatal(err)
			}
			foundReference, foundContext := false, false
			for _, d := range result.Diagnostics {
				if d.Start.Line != 4 {
					t.Fatalf("valid env index produced a diagnostic: %+v", d)
				}
				switch d.Rule {
				case "expression-reference":
					foundReference = strings.Contains(d.Message, `"a"`)
				case "context-availability":
					foundContext = strings.Contains(d.Message, `"matrix"`)
				default:
					t.Fatalf("dynamic index produced an unexpected diagnostic: %+v", d)
				}
			}
			if len(result.Diagnostics) != 2 || !foundReference || !foundContext {
				t.Fatalf("wanted undefined a and unavailable matrix, got %+v", result.Diagnostics)
			}
		})
	}
}
