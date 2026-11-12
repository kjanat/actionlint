package conditions

import (
	"testing"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

func TestAlwaysIgnoresCancellation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		condition string
		want      bool
	}{
		{"always()", true},
		{"${{ always() }}", true},
		{"ALWAYS()", true},
		{"always() && github.ref == 'refs/heads/master'", true},
		{"always() || !cancelled()", true},
		{"always() && (!cancelled() || github.ref == 'refs/heads/master')", true},
		{"always() && cancelled()", true},
		{"always() == true", true},
		{"!cancelled()", false},
		{"cancelled()", false},
		{"contains(github.ref, 'always()')", false},
		{"always() && !cancelled()", false},
		{"!cancelled() && always()", false},
		{"always() && github.ref && !cancelled()", false},
		{"(always() || github.ref) && !cancelled()", false},
		{"always() && cancelled() == false", false},
		{"always() && false", false},
		{"!always()", false},
		{"always() == false", false},
		{"always() != true", false},
		{"(always() && 'x') == true", false},
		{"(always() || 'x') == true", true},
		{"always(1)", false},
	} {
		t.Run(tc.condition, func(t *testing.T) {
			t.Parallel()
			expr, err, _ := exprsyntax.ParseConditionExpression(tc.condition)
			if err != nil || expr == nil {
				t.Fatalf("parse: %v", err)
			}
			if got := AlwaysIgnoresCancellation(expr); got != tc.want {
				t.Fatalf("AlwaysIgnoresCancellation() = %v, want %v", got, tc.want)
			}
		})
	}
}
