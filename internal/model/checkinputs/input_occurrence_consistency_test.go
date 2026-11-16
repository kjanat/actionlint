package checkinputs

import (
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/invocationfixture"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

func TestInputRejectsCorruptCanonicalOccurrence(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(*invocation.Script){
		"unknown id":  func(value *invocation.Script) { value.ID = "unknown" },
		"owner":       func(value *invocation.Script) { value.Owner = "foreign" },
		"declaration": func(value *invocation.Script) { value.Declaration.Node = "foreign" },
		"kind":        func(value *invocation.Script) { value.Kind = invocation.ScriptActionInput },
		"input":       func(value *invocation.Script) { value.Input = "foreign" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			graph := invocationfixture.Graph(t, invocationfixture.Script{Node: "script"})
			occurrence := graph.Scripts()[0]
			input := AnalyzerInput{
				Reference: graph.Entry().Reference, CallGraph: graph,
				AvailableInputs: []rule.InputKind{rule.Invocations, rule.Scripts},
				Invoked:         []Script{{Reference: occurrence.Declaration, Occurrence: occurrence}},
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
