package actionlint

import (
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestStrategyExpressionParallelism(t *testing.T) {
	for _, tc := range []struct{ name, value, message string }{
		{"fraction", "1.5", "must be an integer"},
		{"fraction below one", "0.5", "must be an integer"},
		{"zero", "0", "must be greater than zero"},
		{"negative", "-2", "must be greater than zero"},
		{"positive", "2", ""},
		{"integral float", "2.0", ""},
		{"exponent", "2e1", ""},
	} {
		for _, form := range []string{"strategy", "field", "JSON field"} {
			t.Run(form+"/"+tc.name, func(t *testing.T) {
				field := fmt.Sprintf(`strategy: ${{ fromJSON('{"Max-Parallel":%s}') }}`, tc.value)
				switch form {
				case "field":
					field = "strategy:\n      max-parallel: ${{ " + tc.value + " }}"
				case "JSON field":
					field = "strategy:\n      max-parallel: ${{ fromJSON('" + tc.value + "') }}"
				}
				lint, err := NewLinter(io.Discard, &LinterOptions{Shellcheck: "", Pyflakes: ""})
				if err != nil {
					t.Fatal(err)
				}
				source := "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    " + field + "\n    steps:\n      - run: echo ok\n"
				errs, err := lint.Lint("workflow.yml", []byte(source), nil)
				if err != nil {
					t.Fatal(err)
				}
				if tc.message == "" {
					if len(errs) != 0 {
						t.Fatalf("valid parallelism rejected: %v", errs)
					}
				} else if len(errs) != 1 || errs[0].Kind != "expression" || !strings.Contains(errs[0].Message, tc.message) {
					t.Fatalf("want one %q diagnostic, got %v", tc.message, errs)
				}
			})
		}
	}
}
