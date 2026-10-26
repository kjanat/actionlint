package analyzer

import (
	"errors"
	"testing"
)

func TestInputRejectsDisagreementWithInvocationGraph(t *testing.T) {
	for name, change := range map[string]func(*Input){
		"graph absent":       func(input *Input) { input.CallGraph = nil },
		"fact absent":        func(input *Input) { input.Available = []Fact{Scripts} },
		"text fact absent":   func(input *Input) { input.Available = []Fact{Invocations} },
		"entry reference":    func(input *Input) { input.Reference.Node = "another" },
		"unknown occurrence": func(input *Input) { input.Invoked[0].Occurrence.ID = "unknown" },
		"reference":          func(input *Input) { input.Invoked[0].Reference.Revision = "stale" },
		"ancestry":           func(input *Input) { input.Invoked[0].Occurrence.Ancestry[0] = "unknown" },
		"owner":              func(input *Input) { input.Invoked[0].Occurrence.Owner = input.CallGraph.Entry.ID },
		"duplicate":          func(input *Input) { input.Invoked = append(input.Invoked, input.Invoked[0]) },
		"root collection":    func(input *Input) { input.Scripts, input.Invoked = input.Invoked, nil },
	} {
		t.Run(name, func(t *testing.T) {
			graph := graphFixture(t)
			occurrence := graph.Clone().Scripts[0]
			input := Input{Reference: graph.Entry.Reference, Available: []Fact{Scripts, Invocations}, CallGraph: &graph, Invoked: []Script{{Reference: occurrence.Declaration, Occurrence: occurrence}}}
			if err := input.ValidateInvocations(); err != nil {
				t.Fatal(err)
			}
			change(&input)
			if err := input.ValidateInvocations(); !errors.Is(err, ErrInvalidCallGraph) {
				t.Fatalf("invalid %s accepted: %v", name, err)
			}
		})
	}
}
