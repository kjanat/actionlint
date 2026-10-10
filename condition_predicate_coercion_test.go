package actionlint

import (
	"strings"
	"testing"
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
			expr := parseAssignedExpression("${{ " + tc.expression + " }}")
			if expr == nil {
				t.Fatal("invalid expression fixture")
			}
			value, known := conditionConstantValue(expr)
			if !known || value != tc.want {
				t.Fatalf("value=%v known=%v, want %v", value, known, tc.want)
			}
			for _, condition := range []string{tc.expression, "${{ " + tc.expression + " }}"} {
				if enabled, known := invocationCondition(&String{Value: condition}); !known || enabled != tc.want {
					t.Fatalf("invocation=%v known=%v, want %v", enabled, known, tc.want)
				}
			}
			want := "always falsy"
			if tc.want {
				want = "always truthy"
			}
			source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n        if: ${{ " + tc.expression + " }}\n"
			findings := lintCachePolicy(t, source, "")
			if len(findings) != 1 || !strings.Contains(findings[0].Message, want) {
				t.Fatalf("want %q, got %v", want, findings)
			}
		})
	}
}

func TestConstantPredicateAmbiguousNumbersStayUnknown(t *testing.T) {
	for _, expression := range []string{
		"contains(-0, '0')", "startsWith(-0.0, '-')",
		"contains(1e15, '1')", "contains(1e-5, '0')",
		"endsWith(0.12345678901234567, '7')", "contains(fromJSON('1e309'), 'Infinity')",
		"contains(fromJSON('{}'), 'Object')", "startsWith(fromJSON('[]'), 'Array')",
		"contains(inputs.number, '1')",
	} {
		t.Run(expression, func(t *testing.T) {
			value, known := conditionConstantValue(parseAssignedExpression("${{ " + expression + " }}"))
			if known {
				t.Fatalf("unexpected constant value %v", value)
			}
		})
	}
}
