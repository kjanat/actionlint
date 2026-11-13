package evaluate

import (
	"reflect"
	"testing"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

func TestFunctionArgumentEvaluationOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		input     string
		variables map[string]any
		want      any
		known     bool
		order     []string
	}{
		{"format caches repeated arguments", "format('{1}{0}{1}', first, second)", map[string]any{"first": "zero", "second": "one"}, "onezeroone", true, []string{"second", "first"}},
		{"single item join skips separator", "join(items, unused)", map[string]any{"items": []any{"single"}}, "single", true, []string{"items"}},
		{"empty contains skips needle", "contains(items, unused)", map[string]any{"items": []any{}}, false, true, []string{"items"}},
		{"case skips unselected branch", "case(flag, unused, chosen)", map[string]any{"flag": false, "chosen": "fallback"}, "fallback", true, []string{"flag", "chosen"}},
		{"object predicate skips pattern", "startsWith(object, unused)", map[string]any{"object": map[string]any{}}, false, true, []string{"object"}},
		{"format reads value before rejecting specifier", "format('{0:bad}', value)", map[string]any{"value": "text"}, nil, false, []string{"value"}},
		{"array membership is exact", "contains(fromJSON('[\"push\",\"pull_request\"]'), event)", map[string]any{"event": "pull_request_target"}, false, true, []string{"event"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			node := exprsyntax.ParseAssignedExpression("${{ " + tc.input + " }}")
			if node == nil {
				t.Fatal("expression did not parse")
			}
			var order []string
			got, known := ExpressionValue(node, func(name string) (any, bool) {
				order = append(order, name)
				value, found := tc.variables[name]
				return value, found
			})
			if known != tc.known || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("result = (%v, %t), want (%v, %t)", got, known, tc.want, tc.known)
			}
			if !reflect.DeepEqual(order, tc.order) {
				t.Fatalf("variable requests = %v, want %v", order, tc.order)
			}
		})
	}
}
