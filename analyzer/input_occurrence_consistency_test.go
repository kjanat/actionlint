package analyzer

import (
	"errors"
	"testing"
)

func TestInputRejectsCorruptCanonicalOccurrence(t *testing.T) {
	for name, change := range map[string]func(*ScriptOccurrence){
		"unknown id":  func(value *ScriptOccurrence) { value.ID = OccurrenceID("unknown") },
		"owner":       func(value *ScriptOccurrence) { value.Owner = OccurrenceID("foreign") },
		"ancestry":    func(value *ScriptOccurrence) { value.Ancestry[0] = OccurrenceID("foreign") },
		"depth":       func(value *ScriptOccurrence) { value.Depth++ },
		"declaration": func(value *ScriptOccurrence) { value.Declaration.Node = "foreign" },
		"kind":        func(value *ScriptOccurrence) { value.Kind = ActionInputScript },
		"input":       func(value *ScriptOccurrence) { value.Input = "foreign" },
	} {
		t.Run(name, func(t *testing.T) {
			records := graphFixture(t)
			occurrence := records.Scripts[0]
			input := Input{
				Reference: records.Entry.Reference, CallGraph: retainGraph(t, records),
				Available: []Fact{Invocations, Scripts},
				Invoked:   []Script{{Reference: occurrence.Declaration, Occurrence: occurrence}},
			}
			if err := input.ValidateInvocations(); err != nil {
				t.Fatalf("unmodified input is invalid: %v", err)
			}
			change(&input.Invoked[0].Occurrence)
			if err := input.ValidateInvocations(); !errors.Is(err, ErrInvalidCallGraph) {
				t.Fatalf("corrupt canonical occurrence accepted: %v", err)
			}
		})
	}
}
