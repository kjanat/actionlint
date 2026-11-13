package expressions_test

import (
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestRuleExpressionBareFunctionCondition(t *testing.T) {
	t.Parallel()
	for _, location := range []string{"job", "step"} {
		for _, condition := range []string{"always", "${{ always }}", "always()", "unrelated"} {
			t.Run(location+"/"+condition, func(t *testing.T) {
				t.Parallel()
				job, step := "", ""
				line, column := 4, 9
				if location == "job" {
					job = "    if: " + condition + "\n"
				} else {
					step = "        if: " + condition + "\n"
					line, column = 7, 13
				}
				if strings.HasPrefix(condition, "${{") {
					column += 4
				}
				src := fmt.Sprintf("on: push\njobs:\n  test:\n%s    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n%spermissions: {}\n", job, step)
				result, err := checking.Analyze(t.Context(), checking.Options{WorkingDir: t.TempDir()}, []workspace.Source{{Path: "ci.yml", Content: []byte(src)}})
				if err != nil {
					t.Fatal(err)
				}
				if condition == "always()" {
					if len(result.Diagnostics) != 0 {
						t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
					}
					return
				}
				want := `function "always" must be called with parentheses; did you mean "always()"?`
				if condition == "unrelated" {
					want = `undefined variable "unrelated". available variables are `
				}
				if len(result.Diagnostics) != 1 {
					t.Fatalf("want one diagnostic, got %+v", result.Diagnostics)
				}
				d := result.Diagnostics[0]
				wantRule := "expression-call"
				if condition == "unrelated" {
					wantRule = "expression-reference"
				}
				if d.Rule != wantRule || !strings.HasPrefix(d.Message, want) || d.Start.Line != line || d.Start.Column != column {
					t.Fatalf("unexpected diagnostic: %+v", d)
				}
			})
		}
	}
}
