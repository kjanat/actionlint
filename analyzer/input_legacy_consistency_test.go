package analyzer

import (
	"errors"
	"testing"
)

func TestInputRejectsConflictingLegacyInvocation(t *testing.T) {
	for name, change := range map[string]func(*Invocation){
		"matching":  func(*Invocation) {},
		"reference": func(value *Invocation) { value.Reference.Node = "foreign" },
		"depth":     func(value *Invocation) { value.Depth++ },
		"step":      func(value *Invocation) { value.Step.Line++ },
		"uses":      func(value *Invocation) { value.Uses = "other/action@v1" },
		"caller":    func(value *Invocation) { value.Caller = "other.yml" },
	} {
		t.Run(name, func(t *testing.T) {
			records := graphFixture(t)
			call := records.Calls[0]
			occurrence := records.Scripts[0]
			legacy := &Invocation{Reference: call.Site, Depth: call.Depth, Step: call.Step, Uses: call.Uses}
			input := Input{Reference: records.Entry.Reference, CallGraph: retainGraph(t, records), Available: []Fact{Invocations, Scripts}, Invoked: []Script{{Reference: occurrence.Declaration, Occurrence: occurrence, Invocation: legacy}}}
			change(legacy)
			err := input.ValidateInvocations()
			if name == "matching" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, ErrInvalidCallGraph) {
				t.Fatalf("conflicting legacy data accepted: %v", err)
			}
		})
	}
}
