package typecheck

import (
	"testing"

	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

func TestEventProjectionIsNotMutated(t *testing.T) {
	array := exprtypes.NewArrayType(exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"message": exprtypes.StringType()}).Type())
	event := exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"commits": array.Type()})
	checker := NewChecker(contextspec.DefaultExpressionScope(), nil)
	checker.SetContextAvailability([]string{"github"})
	checker.UpdateEvent(event)
	node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer("github.event.commits.*.message}}"))
	if err != nil {
		t.Fatal(err)
	}
	if _, errs := checker.Check(node); len(errs) != 0 {
		t.Fatal(errs)
	}
	if array.Deref() {
		t.Fatal("filter mutated shared array type")
	}
	checker.UpdateDispatchInputs(exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"flag": exprtypes.BoolType()}))
	if _, ok := event.Property("inputs"); ok {
		t.Fatal("dispatch update mutated shared event")
	}
}
