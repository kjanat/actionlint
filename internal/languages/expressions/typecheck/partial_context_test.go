package typecheck

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

func TestPartialContextLookup(t *testing.T) {
	t.Parallel()
	lookup := func(name string) (any, bool) { return "Windows", name == "runner.os" }
	for _, tc := range []struct {
		input string
		known bool
		value any
	}{
		{"runner.os == 'Windows'", true, true},
		{"runner['os'] == 'Windows'", true, true},
		{"contains(fromJSON('[\"Linux\"]'), runner.os)", true, false},
		{"runner.arch != 'X64'", false, nil},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			node, err := syntax.NewParser().ParseExpression(syntax.NewLexer(tc.input + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			got, known := evaluate.ExpressionValueWithLookup(node, lookup)
			if known != tc.known || got != tc.value {
				t.Fatalf("got %v/%v, want %v/%v", got, known, tc.value, tc.known)
			}
			if _, known := evaluate.ExpressionValue(node, lookup); known {
				t.Fatal("legacy root lookup unexpectedly consumed a property path")
			}
		})
	}
}
