package analyzer

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/source"
)

func graphFixture(t *testing.T) CallGraph {
	t.Helper()
	entry, call, script := OccurrenceID(strings.Repeat("a", 64)), OccurrenceID(strings.Repeat("b", 64)), OccurrenceID(strings.Repeat("c", 64))
	outcome, err := NewCallOutcome(ExpandedCall{Target: source.Reference{Document: "action", Revision: "r", Node: "document"}})
	if err != nil {
		t.Fatal(err)
	}
	return CallGraph{
		Entry:  InvocationEntry{ID: entry, Kind: WorkflowEntry, Reference: source.Reference{Document: "workflow", Revision: "r", Node: "document"}},
		Budget: InvocationBudget{MaxDepth: 1, MaxCalls: 1, MaxScripts: 1}, Complete: true,
		Calls:   []Call{{ID: call, Parent: entry, Depth: 1, Ancestry: []OccurrenceID{entry}, Site: source.Reference{Document: "workflow", Revision: "r", Node: "call"}, Condition: CallEnabled, Outcome: outcome}},
		Scripts: []ScriptOccurrence{{ID: script, Owner: call, Depth: 1, Ancestry: []OccurrenceID{entry, call}, Declaration: source.Reference{Document: "action", Revision: "r", Node: "script"}, Kind: RunScript}},
	}
}

func TestCallGraphRejectsMalformedTransportValues(t *testing.T) {
	for name, change := range map[string]func(*CallGraph){
		"entry ID":         func(g *CallGraph) { g.Entry.ID = "unknown" },
		"entry kind":       func(g *CallGraph) { g.Entry.Kind = "unknown" },
		"budget":           func(g *CallGraph) { g.Budget.MaxScripts = 0 },
		"duplicate ID":     func(g *CallGraph) { g.Scripts[0].ID = g.Calls[0].ID },
		"unknown parent":   func(g *CallGraph) { g.Calls[0].Parent = "unknown" },
		"ancestry":         func(g *CallGraph) { g.Calls[0].Ancestry[0] = g.Calls[0].ID },
		"depth":            func(g *CallGraph) { g.Scripts[0].Depth = 0 },
		"source revision":  func(g *CallGraph) { g.Scripts[0].Declaration.Revision = "stale" },
		"call source":      func(g *CallGraph) { g.Calls[0].Site.Document = "foreign" },
		"script source":    func(g *CallGraph) { g.Scripts[0].Declaration.Document = "foreign" },
		"unknown owner":    func(g *CallGraph) { g.Scripts[0].Owner = "unknown" },
		"script ancestry":  func(g *CallGraph) { g.Scripts[0].Ancestry = []OccurrenceID{g.Entry.ID} },
		"script kind":      func(g *CallGraph) { g.Scripts[0].Kind = "unknown" },
		"script input":     func(g *CallGraph) { g.Scripts[0].Input = "script" },
		"condition":        func(g *CallGraph) { g.Calls[0].Condition = "unknown" },
		"missing uses":     func(g *CallGraph) { g.Calls[0].Uses = "$/action" },
		"partial position": func(g *CallGraph) { g.Calls[0].Step.Line = 1 },
		"zero outcome":     func(g *CallGraph) { g.Calls[0].Outcome = CallOutcome{} },
		"completion":       func(g *CallGraph) { g.Complete = false },
		"stop owner": func(g *CallGraph) {
			g.Stop = Present(InvocationStop{Parent: "unknown", Limit: CallLimit, Max: 1})
			g.Complete = false
		},
		"stop limit": func(g *CallGraph) {
			g.Stop = Present(InvocationStop{Parent: g.Entry.ID, Site: g.Calls[0].Site, Limit: "depth", Max: 1})
			g.Complete = false
		},
		"stop count": func(g *CallGraph) {
			g.Stop = Present(InvocationStop{Parent: g.Entry.ID, Site: g.Calls[0].Site, Limit: CallLimit, Max: 2})
			g.Complete = false
		},
		"interruption": func(g *CallGraph) { g.EntryInterruption = Present(Interruption{}); g.Complete = false },
	} {
		t.Run(name, func(t *testing.T) {
			graph := graphFixture(t)
			if err := graph.Validate(); err != nil {
				t.Fatal(err)
			}
			change(&graph)
			if err := graph.Validate(); !errors.Is(err, ErrInvalidCallGraph) {
				t.Fatalf("malformed %s accepted: %v", name, err)
			}
		})
	}
}

func TestCallGraphCloneSeparatesOverlappingAncestry(t *testing.T) {
	graph := graphFixture(t)
	graph.Calls[0].Ancestry = graph.Scripts[0].Ancestry[:1]
	clone := graph.Clone()
	clone.Calls[0].Ancestry[0] = "changed"
	if graph.Calls[0].Ancestry[0] != graph.Entry.ID || clone.Scripts[0].Ancestry[0] != graph.Entry.ID {
		t.Fatal("clone retained overlapping storage")
	}
	clone.Scripts[0].Ancestry = append(clone.Scripts[0].Ancestry, "changed")
	if len(graph.Scripts[0].Ancestry) != 2 {
		t.Fatal("append reached source")
	}
	if empty := (CallGraph{}).Clone(); !reflect.DeepEqual(empty, CallGraph{}) {
		t.Fatal("clone changed nil slices")
	}
}
