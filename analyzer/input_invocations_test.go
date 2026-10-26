package analyzer

import (
	"errors"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/source"
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
		"missing text":       func(input *Input) { input.Invoked = nil },
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

func TestInputScriptSelectionCoverage(t *testing.T) {
	graph := graphFixture(t)
	second := graph.Clone().Scripts[0]
	second.ID = OccurrenceID(strings.Repeat("d", 64))
	second.Declaration.Node = "second"
	graph.Scripts = append(graph.Scripts, second)
	graph.Budget.MaxScripts = 2
	first := graph.Scripts[0]
	for _, test := range []struct {
		name      string
		selection []OccurrenceID
		scripts   []Script
		valid     bool
	}{
		{"all", nil, []Script{{Reference: first.Declaration, Occurrence: first}, {Reference: second.Declaration, Occurrence: second}}, true},
		{"subset", []OccurrenceID{first.ID}, []Script{{Reference: first.Declaration, Occurrence: first}}, true},
		{"none", []OccurrenceID{}, nil, true},
		{"omitted", nil, []Script{{Reference: first.Declaration, Occurrence: first}}, false},
		{"selected missing", []OccurrenceID{first.ID}, nil, false},
		{"unselected supplied", []OccurrenceID{}, []Script{{Reference: first.Declaration, Occurrence: first}}, false},
		{"duplicate selection", []OccurrenceID{first.ID, first.ID}, []Script{{Reference: first.Declaration, Occurrence: first}}, false},
		{"unknown selection", []OccurrenceID{"unknown"}, nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := Input{Reference: graph.Entry.Reference, Available: []Fact{Invocations, Scripts}, CallGraph: &graph, SelectedScripts: test.selection, Invoked: test.scripts}
			if err := input.ValidateInvocations(); (err == nil) != test.valid {
				t.Fatalf("valid=%v: %v", test.valid, err)
			}
		})
	}
	input := Input{Reference: graph.Entry.Reference, Available: []Fact{Invocations}, CallGraph: &graph}
	if err := input.ValidateInvocations(); err != nil {
		t.Fatalf("graph without collected text: %v", err)
	}
	input.SelectedScripts = []OccurrenceID{}
	if err := input.ValidateInvocations(); !errors.Is(err, ErrInvalidCallGraph) {
		t.Fatalf("selection without text fact accepted: %v", err)
	}
}

func TestInputRejectsOrphanedOccurrenceFields(t *testing.T) {
	for name, occurrence := range map[string]ScriptOccurrence{
		"id": {ID: "id"}, "owner": {Owner: "owner"}, "ancestry": {Ancestry: []OccurrenceID{"owner"}},
		"depth": {Depth: 1}, "declaration": {Declaration: source.Reference{Document: "document"}},
		"kind": {Kind: RunScript}, "input": {Input: "script"},
	} {
		t.Run(name, func(t *testing.T) {
			for _, input := range []Input{
				{Available: []Fact{Scripts}, Scripts: []Script{{Occurrence: occurrence}}},
				{Available: []Fact{Scripts}, Invoked: []Script{{Occurrence: occurrence}}},
				{Available: []Fact{Scripts}, ActionInputs: []ActionInput{{Occurrence: occurrence}}},
			} {
				if err := input.ValidateInvocations(); !errors.Is(err, ErrInvalidCallGraph) {
					t.Fatalf("orphan occurrence accepted: %v", err)
				}
			}
		})
	}
	for _, input := range []Input{{Scripts: []Script{{Text: "text"}}}, {SelectedScripts: []OccurrenceID{}}} {
		if err := input.ValidateInvocations(); !errors.Is(err, ErrInvalidCallGraph) {
			t.Fatalf("orphan selection or unavailable text accepted: %v", err)
		}
	}
}
