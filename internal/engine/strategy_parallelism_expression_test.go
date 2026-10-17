package engine

import (
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/tools/adapters"
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
				request := Request{Tools: (adapters.Options{ShellCheck: ""}).Registrations()}
				var err error

				source := `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    ` + field + `
    steps:
      - run: echo ok
`
				request.Sources = []SourceUnit{{Path: "workflow.yml", InputPath: "workflow.yml", Content: []byte(source), Project: nil}}
				errs, err := testFindings(runTestAnalysis(t.Context(), request))
				if err != nil {
					t.Fatal(err)
				}
				if tc.message == "" {
					if len(errs) != 0 {
						t.Fatalf("valid parallelism rejected: %v", errs)
					}
				} else if len(errs) != 1 || errs[0].Rule != "expression" || !strings.Contains(errs[0].Message, tc.message) {
					t.Fatalf("want one %q diagnostic, got %v", tc.message, errs)
				}
			})
		}
	}
}
