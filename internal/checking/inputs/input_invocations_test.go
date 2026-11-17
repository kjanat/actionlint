package inputs

import (
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/support/invocationfixture"
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	rulemeta "actionlint.kjanat.dev/v2/internal/model/rules"
)

func TestInputRejectsDisagreementWithInvocationGraph(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(*AnalyzerInput){
		"graph absent":             func(input *AnalyzerInput) { input.CallGraph = invocation.Graph{} },
		"invocations input absent": func(input *AnalyzerInput) { input.AvailableInputs = []rulemeta.InputKind{rulemeta.Scripts} },
		"scripts input absent":     func(input *AnalyzerInput) { input.AvailableInputs = []rulemeta.InputKind{rulemeta.Invocations} },
		"entry reference":          func(input *AnalyzerInput) { input.Reference.Node = "another" },
		"unknown occurrence":       func(input *AnalyzerInput) { input.Invoked[0].Occurrence.ID = "unknown" },
		"reference":                func(input *AnalyzerInput) { input.Invoked[0].Reference.Revision = "stale" },
		"owner":                    func(input *AnalyzerInput) { input.Invoked[0].Occurrence.Owner = input.CallGraph.RootID() },
		"duplicate":                func(input *AnalyzerInput) { input.Invoked = append(input.Invoked, input.Invoked[0]) },
		"root collection":          func(input *AnalyzerInput) { input.Scripts, input.Invoked = input.Invoked, nil },
		"missing text":             func(input *AnalyzerInput) { input.Invoked = nil },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			graph := invocationfixture.Graph(t, invocationfixture.Script{Node: "script"})
			occurrence := graph.Scripts()[0]
			input := AnalyzerInput{Reference: graph.Entry().Reference, AvailableInputs: []rulemeta.InputKind{rulemeta.Scripts, rulemeta.Invocations}, CallGraph: graph, Invoked: []Script{{Reference: occurrence.Declaration, Occurrence: occurrence}}}
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
	t.Parallel()
	graph := invocationfixture.Graph(t, invocationfixture.Script{Node: "script"}, invocationfixture.Script{Node: "second"})
	first, second := graph.Scripts()[0], graph.Scripts()[1]
	for _, test := range []struct {
		name      string
		selection []invocation.ID
		scripts   []Script
		valid     bool
	}{
		{"all", nil, []Script{{Reference: first.Declaration, Occurrence: first}, {Reference: second.Declaration, Occurrence: second}}, true},
		{"subset", []invocation.ID{first.ID}, []Script{{Reference: first.Declaration, Occurrence: first}}, true},
		{"none", []invocation.ID{}, nil, true},
		{"omitted", nil, []Script{{Reference: first.Declaration, Occurrence: first}}, false},
		{"selected missing", []invocation.ID{first.ID}, nil, false},
		{"unselected supplied", []invocation.ID{}, []Script{{Reference: first.Declaration, Occurrence: first}}, false},
		{"duplicate selection", []invocation.ID{first.ID, first.ID}, []Script{{Reference: first.Declaration, Occurrence: first}}, false},
		{"unknown selection", []invocation.ID{"unknown"}, nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := AnalyzerInput{Reference: graph.Entry().Reference, AvailableInputs: []rulemeta.InputKind{rulemeta.Invocations, rulemeta.Scripts}, CallGraph: graph, SelectedScripts: test.selection, Invoked: test.scripts}
			if err := input.ValidateInvocations(); (err == nil) != test.valid {
				t.Fatalf("valid=%v: %v", test.valid, err)
			}
		})
	}
	input := AnalyzerInput{Reference: graph.Entry().Reference, AvailableInputs: []rulemeta.InputKind{rulemeta.Invocations}, CallGraph: graph}
	if err := input.ValidateInvocations(); err != nil {
		t.Fatalf("graph without collected text: %v", err)
	}
	input.SelectedScripts = []invocation.ID{}
	if err := input.ValidateInvocations(); !errors.Is(err, ErrInvalidCallGraph) {
		t.Fatalf("selection without the scripts input accepted: %v", err)
	}
}

func TestInputRejectsOrphanedOccurrenceFields(t *testing.T) {
	t.Parallel()
	for name, occurrence := range map[string]invocation.Script{
		"id": {ID: "id"}, "owner": {Owner: "owner"}, "declaration": {Declaration: source.Reference{Document: "document"}},
		"kind": {Kind: invocation.ScriptRun}, "input": {Input: "script"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, input := range []AnalyzerInput{
				{AvailableInputs: []rulemeta.InputKind{rulemeta.Scripts}, Scripts: []Script{{Occurrence: occurrence}}},
				{AvailableInputs: []rulemeta.InputKind{rulemeta.Scripts}, Invoked: []Script{{Occurrence: occurrence}}},
				{AvailableInputs: []rulemeta.InputKind{rulemeta.Scripts}, ActionInputs: []ActionInput{{Occurrence: occurrence}}},
			} {
				if err := input.ValidateInvocations(); !errors.Is(err, ErrInvalidCallGraph) {
					t.Fatalf("orphan occurrence accepted: %v", err)
				}
			}
		})
	}
	for _, input := range []AnalyzerInput{{Scripts: []Script{{Text: "text"}}}, {SelectedScripts: []invocation.ID{}}} {
		if err := input.ValidateInvocations(); !errors.Is(err, ErrInvalidCallGraph) {
			t.Fatalf("orphan selection or unavailable text accepted: %v", err)
		}
	}
}
