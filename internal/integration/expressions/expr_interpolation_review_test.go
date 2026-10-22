package expressions_test

import (
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestInterpolationChecksContinueAfterSemanticErrors(t *testing.T) {
	value := "${{ unknown.value }} ${{ github.ref && '' || 'fallback' }}"
	for _, field := range []struct {
		name, workflow string
	}{
		{"workflow env", `on: push
env:
  VALUE: ` + value + `
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`},
		{"job env", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    env:
      VALUE: ` + value + `
    steps:
      - run: echo ok
`},
		{"step env", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - env:
          VALUE: ` + value + `
        run: echo ok
`},
		{"step name", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: ` + value + `
        run: echo ok
`},
		{"action input", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
        with:
          path: ` + value + "\n"},
		{"matrix string", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        value:
          - ` + value + `
    steps:
      - run: echo ok
`},
	} {
		for _, levels := range []struct {
			expression, ternary string
			wantExpression      bool
			wantTernary         bool
		}{
			{"off", "on", false, true},
			{"on", "on", true, true},
			{"off", "warn", false, true},
			{"on", "off", true, false},
			{"off", "off", false, false},
		} {
			t.Run(field.name+"/"+levels.expression+"/"+levels.ternary, func(t *testing.T) {
				cfg, err := repoconfig.Parse([]byte("lint: {rules: {correctness: {expression-reference: " + levels.expression + ", unsound-ternary: " + levels.ternary + "}}}"))
				if err != nil {
					t.Fatal(err)
				}
				for _, ending := range []string{"\n", "\r\n"} {
					workflow := strings.ReplaceAll(field.workflow, "\n", ending)
					result, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir()}, []workspace.Source{{Path: "workflow.yaml", Content: []byte(workflow), Config: cfg}})
					if err != nil {
						t.Fatal(err)
					}
					found := map[string]int{}
					for _, diagnostic := range result.Diagnostics {
						found[diagnostic.Rule]++
						if diagnostic.Rule != "unsound-ternary" {
							continue
						}
						before, _, foundOperand := strings.Cut(workflow, "'' ||")
						if !foundOperand {
							t.Fatal("test workflow is missing the falsy operand")
						}
						line := strings.Count(before, "\n") + 1
						column := len(before) - strings.LastIndex(before, "\n")
						if diagnostic.Start != (diagmodel.Position{Line: line, Column: column}) {
							t.Errorf("later interpolation location: got %+v, want %d:%d", diagnostic.Start, line, column)
						}
						if levels.ternary == "warn" && diagnostic.Severity != "warning" {
							t.Errorf("independent warning level lost: %+v", diagnostic)
						}
					}
					if (found["expression-reference"] != 0) != levels.wantExpression || (found["unsound-ternary"] != 0) != levels.wantTernary ||
						found["expression-reference"] > 1 || found["unsound-ternary"] > 1 || len(found) > 2 {
						t.Fatalf("independent interpolation diagnostics lost: %+v", result.Diagnostics)
					}
				}
			})
		}
	}
}
