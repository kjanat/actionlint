package typecheck

import (
	"strings"
	"testing"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	runnerjson "actionlint.kjanat.dev/v2/internal/languages/json"
)

func TestJSONMemberCollisions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input string
		count int
	}{
		{`{"approved":false,"APPROVED":true}`, 1},
		{`{"approved":false,"approved":true}`, 1},
		{`[{"a":1,"A":2},{"nested":{"b":1,"B":2}}]`, 2},
		{`{"\u0061":1,"A":2}`, 1},
		{`{"É":1,"é":2}`, 1},
		{`{"ß":1,"SS":2}`, 0},
		{`{"ı":1,"I":2,"ſ":3,"S":4}`, 0},
		{`[{"a":1},{"A":2}]`, 0},
		{`{"a":{"A":2}}`, 0},
		{`{"approved":false,"denied":true}`, 0},
		{`{"a":1,"A":2} trailing`, 1},
		{`{"a":1,"A":2} {}`, 1},
		{`{"a":1,"A":2} null`, 1},
		{`[{"a":1,"A":2}] false`, 1},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			collisions := runnerjson.JSONMemberCollisions(tc.input)
			if len(collisions) != tc.count {
				t.Fatalf("got %v", collisions)
			}
			expr := exprsyntax.ParseAssignedExpression("${{ fromJSON('" + tc.input + "') }}")
			_, errs := NewChecker(contextspec.DefaultExpressionScope(), nil).Check(expreval.EvaluateSubexpressions(expr))
			if len(errs) != tc.count {
				t.Fatal(errs)
			}
			for _, err := range errs {
				if !strings.Contains(err.Message, "collide under case-insensitive lookup") {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestJSONCollisionsDoNotMaskInvalidJSON(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		`{"a":1,"A":2`, `[{"a":1,"A":2} false]`,
		`{"a":1,"A":2,,}`, `{"a":1,"A":2 /* unterminated`,
	} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if got := runnerjson.JSONMemberCollisions(input); len(got) != 0 {
				t.Fatal(got)
			}
			expr := exprsyntax.ParseAssignedExpression("${{ fromJSON('" + input + "') }}")
			_, errs := NewChecker(contextspec.DefaultExpressionScope(), nil).Check(expreval.EvaluateSubexpressions(expr))
			if len(errs) != 1 || !strings.Contains(errs[0].Message, "broken JSON string") {
				t.Fatal(errs)
			}
		})
	}
	if got := runnerjson.JSONMemberCollisions("{\"a\":1,\"A\":2} \n\t"); len(got) != 1 {
		t.Fatal(got)
	}
}
