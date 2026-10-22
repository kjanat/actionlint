package expressions_test

import (
	"strings"
	"testing"

	exprlang "actionlint.kjanat.dev/internal/expression"
	"actionlint.kjanat.dev/internal/testutil"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workflow/execution"
)

func TestConstantPredicateNumericCoercion(t *testing.T) {
	for _, tc := range []struct {
		expression string
		want       bool
	}{
		{"contains(123, 2)", true},
		{"contains(123, 4)", false},
		{"startsWith(123, 1)", true},
		{"startsWith(123, 2)", false},
		{"endsWith(123, 3)", true},
		{"endsWith(123, 2)", false},
		{"contains('123', 2)", true},
		{"contains(123, '2')", true},
		{"startsWith(-123, '-1')", true},
		{"contains(0, '0')", true},
		{"endsWith(123.0, '23')", true},
		{"contains(999999999999999.0, '999')", true},
		{"contains(fromJSON('[1,2,3]'), 2)", true},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			expr := exprlang.ParseAssignedExpression("${{ " + tc.expression + " }}")
			if expr == nil {
				t.Fatal("invalid expression fixture")
			}
			value, known := exprlang.ConditionConstantValue(expr)
			if !known || value != tc.want {
				t.Fatalf("value=%v known=%v, want %v", value, known, tc.want)
			}
			for _, condition := range []string{tc.expression, "${{ " + tc.expression + " }}"} {
				if enabled, known := execution.Condition(&workflowlang.String{Value: condition}); !known || enabled != tc.want {
					t.Fatalf("invocation=%v known=%v, want %v", enabled, known, tc.want)
				}
			}
			want := "always falsy"
			if tc.want {
				want = "always truthy"
			}
			source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
        if: ${{ ` + tc.expression + " }}\n"
			findings := testutil.WorkflowDiagnostics(t, source, "")
			if len(findings) != 1 || !strings.Contains(findings[0].Message, want) {
				t.Fatalf("want %q, got %v", want, findings)
			}
		})
	}
}
