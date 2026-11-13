package typecheck

import (
	"testing"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

func TestExpressionValueContexts(t *testing.T) {
	t.Parallel()
	variables := func(name string) (any, bool) {
		return map[string]any{"pm": "bun", "nested": map[string]any{"tool": "bun"}, "items": []any{"nodejs", "bun"}}, name == "matrix"
	}
	for _, tc := range []struct {
		expression string
		want       any
		known      bool
	}{
		{"matrix.pm", "bun", true},
		{"matrix.PM", "bun", true},
		{"matrix['pm']", "bun", true},
		{"matrix.nested.tool", "bun", true},
		{"matrix.missing", nil, true},
		{"matrix.missing.child", nil, true},
		{"matrix.items[1]", "bun", true},
		{"matrix.items[20]", nil, true},
		{"matrix.pm == 'bun' && 'yes' || 'no'", "yes", true},
		{"env.TOKEN", nil, false},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			t.Parallel()
			expression := exprsyntax.ParseAssignedExpression("${{ " + tc.expression + " }}")
			got, known := expreval.ExpressionValue(expression, variables)
			if known != tc.known || got != tc.want {
				t.Fatalf("got %v known=%v, want %v known=%v", got, known, tc.want, tc.known)
			}
		})
	}
	if _, known := expreval.ExpressionValue(exprsyntax.ParseAssignedExpression("${{ matrix.pm }}"), nil); known {
		t.Fatal("missing context fabricated a value")
	}
}
