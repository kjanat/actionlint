package expression

import "testing"

func TestEventProjectionIsNotMutated(t *testing.T) {
	array := &ArrayType{Elem: NewStrictObjectType(map[string]Type{"message": StringType{}})}
	event := NewStrictObjectType(map[string]Type{"commits": array})
	checker := NewChecker(false, nil)
	checker.SetContextAvailability([]string{"github"})
	checker.UpdateEvent(event)
	node, err := NewParser().Parse(NewLexer("github.event.commits.*.message}}"))
	if err != nil {
		t.Fatal(err)
	}
	if _, errs := checker.Check(node); len(errs) != 0 {
		t.Fatal(errs)
	}
	if array.Deref {
		t.Fatal("filter mutated shared array type")
	}
	checker.UpdateDispatchInputs(NewStrictObjectType(map[string]Type{"flag": BoolType{}}))
	if _, ok := event.Props["inputs"]; ok {
		t.Fatal("dispatch update mutated shared event")
	}
}
