package expressions_test

import (
	"strings"
	"testing"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"

	value "actionlint.kjanat.dev/v2/internal/model/contexts/values"

	workflowconditions "actionlint.kjanat.dev/v2/internal/model/workflows/conditions"
)

func TestConstantFormatNumericArguments(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ expression, want string }{
		{"format('{0}', 0)", "0"},
		{"format('{0}', 0.0)", "0"},
		{"format('{0}', 123)", "123"},
		{"format('{0}', -123)", "-123"},
		{"format('{0}', 123.0)", "123"},
		{"format('{0}', 1e3)", "1000"},
		{"format('{0}', 999999999999999.0)", "999999999999999"},
		{"format('{0}', -999999999999999.0)", "-999999999999999"},
		{"format('{0}', fromJSON('42'))", "42"},
		{"format('{{{1}}}:{0}:{1}', 0, -12)", "{-12}:0:-12"},
		{"format('{0}:{1}:{2}', 'text', false, null)", "text:false:"},
		{"format('{0}', null)", ""},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			t.Parallel()
			expr := exprsyntax.ParseAssignedExpression("${{ " + tc.expression + " }}")
			if expr == nil {
				t.Fatal("invalid expression fixture")
			}
			constant, known := expreval.ConditionConstantValue(expr)
			if !known || constant != tc.want {
				t.Fatalf("value=%v known=%v, want %q", constant, known, tc.want)
			}
			for _, condition := range []string{tc.expression, "${{ " + tc.expression + " }}"} {
				if enabled, known := workflowconditions.Condition(value.Condition(condition, value.Origin{})); !known || enabled != (tc.want != "") {
					t.Fatalf("invocation=%v known=%v, want %v", enabled, known, tc.want != "")
				}
			}
			want := "always falsy"
			if tc.want != "" {
				want = "always truthy"
			}
			source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
        if: ${{ ` + tc.expression + " }}\n" + "\npermissions: {}\n"
			findings := testutil.WorkflowDiagnostics(t, source, "")
			if len(findings) != 1 || !strings.Contains(findings[0].Message, want) {
				t.Fatalf("want %q, got %v", want, findings)
			}
		})
	}
}
