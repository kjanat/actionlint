package analyzer

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/source"
)

func graphFixture(t *testing.T) CallGraphData {
	t.Helper()
	entry, call, script := OccurrenceID(strings.Repeat("a", 64)), OccurrenceID(strings.Repeat("b", 64)), OccurrenceID(strings.Repeat("c", 64))
	outcome, err := NewCallOutcome(ExpandedCall{Target: source.Reference{Document: "action", Revision: "r", Node: "document"}})
	if err != nil {
		t.Fatal(err)
	}
	return CallGraphData{
		Entry:  InvocationEntry{ID: entry, Kind: WorkflowEntry, Reference: source.Reference{Document: "workflow", Revision: "r", Node: "document"}},
		Budget: InvocationBudget{MaxDepth: 1, MaxCalls: 1, MaxScripts: 1}, Complete: true,
		Calls:   []Call{{ID: call, Parent: entry, Depth: 1, Ancestry: []OccurrenceID{entry}, Site: source.Reference{Document: "workflow", Revision: "r", Node: "call"}, Condition: CallEnabled, Outcome: outcome}},
		Scripts: []ScriptOccurrence{{ID: script, Owner: call, Depth: 1, Ancestry: []OccurrenceID{entry, call}, Declaration: source.Reference{Document: "action", Revision: "r", Node: "script"}, Kind: RunScript}},
	}
}

func TestCallGraphRejectsMalformedTransportValues(t *testing.T) {
	for name, change := range map[string]func(*CallGraphData){
		"entry ID":         func(g *CallGraphData) { g.Entry.ID = "unknown" },
		"entry kind":       func(g *CallGraphData) { g.Entry.Kind = "unknown" },
		"budget":           func(g *CallGraphData) { g.Budget.MaxScripts = 0 },
		"duplicate ID":     func(g *CallGraphData) { g.Scripts[0].ID = g.Calls[0].ID },
		"unknown parent":   func(g *CallGraphData) { g.Calls[0].Parent = "unknown" },
		"ancestry":         func(g *CallGraphData) { g.Calls[0].Ancestry[0] = g.Calls[0].ID },
		"depth":            func(g *CallGraphData) { g.Scripts[0].Depth = 0 },
		"source revision":  func(g *CallGraphData) { g.Scripts[0].Declaration.Revision = "stale" },
		"call source":      func(g *CallGraphData) { g.Calls[0].Site.Document = "foreign" },
		"script source":    func(g *CallGraphData) { g.Scripts[0].Declaration.Document = "foreign" },
		"unknown owner":    func(g *CallGraphData) { g.Scripts[0].Owner = "unknown" },
		"script ancestry":  func(g *CallGraphData) { g.Scripts[0].Ancestry = []OccurrenceID{g.Entry.ID} },
		"script kind":      func(g *CallGraphData) { g.Scripts[0].Kind = "unknown" },
		"script input":     func(g *CallGraphData) { g.Scripts[0].Input = "script" },
		"condition":        func(g *CallGraphData) { g.Calls[0].Condition = "unknown" },
		"missing uses":     func(g *CallGraphData) { g.Calls[0].Uses = "$/action" },
		"partial position": func(g *CallGraphData) { g.Calls[0].Step.Line = 1 },
		"zero outcome":     func(g *CallGraphData) { g.Calls[0].Outcome = CallOutcome{} },
		"completion":       func(g *CallGraphData) { g.Complete = false },
		"stop owner": func(g *CallGraphData) {
			g.Stop = Present(InvocationStop{Parent: "unknown", Limit: CallLimit, Max: 1})
			g.Complete = false
		},
		"stop limit": func(g *CallGraphData) {
			g.Stop = Present(InvocationStop{Parent: g.Entry.ID, Site: g.Calls[0].Site, Limit: "depth", Max: 1})
			g.Complete = false
		},
		"stop count": func(g *CallGraphData) {
			g.Stop = Present(InvocationStop{Parent: g.Entry.ID, Site: g.Calls[0].Site, Limit: CallLimit, Max: 2})
			g.Complete = false
		},
		"interruption": func(g *CallGraphData) { g.EntryInterruption = Present(Interruption{}); g.Complete = false },
	} {
		t.Run(name, func(t *testing.T) {
			graph := graphFixture(t)
			if _, err := NewCallGraph(graph); err != nil {
				t.Fatal(err)
			}
			change(&graph)
			if _, err := NewCallGraph(graph); !errors.Is(err, ErrInvalidCallGraph) {
				t.Fatalf("malformed %s accepted: %v", name, err)
			}
		})
	}
}

func retainGraph(t *testing.T, records CallGraphData) CallGraph {
	t.Helper()
	graph, err := NewCallGraph(records)
	if err != nil {
		t.Fatal(err)
	}
	return graph
}

func TestCallGraphDetachesConstructionAndAccess(t *testing.T) {
	records := graphFixture(t)
	records.Calls[0].Ancestry = records.Scripts[0].Ancestry[:1]
	graph := retainGraph(t, records)
	want := graph.Snapshot()
	records.Calls[0].Ancestry[0] = "changed"
	records.Scripts[0].Declaration.Node = "changed"
	snapshot := graph.Snapshot()
	snapshot.Calls[0].Ancestry[0] = "changed"
	if snapshot.Scripts[0].Ancestry[0] != want.Entry.ID {
		t.Fatal("snapshot records overlap")
	}
	snapshot.Scripts[0].Ancestry = append(snapshot.Scripts[0].Ancestry, "changed")
	call, found := graph.Call(want.Calls[0].ID)
	if !found {
		t.Fatal("call missing")
	}
	call.Ancestry[0] = "changed"
	script, found := graph.Script(want.Scripts[0].ID)
	if !found {
		t.Fatal("script missing")
	}
	script.Ancestry[0] = "changed"
	calls, err := graph.CallsPage(0, MaxCallGraphPageSize)
	if err != nil || len(calls) != 1 {
		t.Fatalf("calls page: %v, %v", calls, err)
	}
	calls[0].Ancestry[0] = "changed"
	scripts, err := graph.ScriptsPage(0, MaxCallGraphPageSize)
	if err != nil || len(scripts) != 1 {
		t.Fatalf("scripts page: %v, %v", scripts, err)
	}
	scripts[0].Ancestry[0] = "changed"
	if !reflect.DeepEqual(graph.Snapshot(), want) {
		t.Fatal("mutable records reached retained graph")
	}
	copyHandle := graph
	graph = CallGraph{}
	if graph.Valid() || !reflect.DeepEqual(copyHandle.Snapshot(), want) {
		t.Fatal("handle assignment mutated shared graph")
	}
}

func TestCallGraphPageBoundaries(t *testing.T) {
	graph := retainGraph(t, graphFixture(t))
	for _, bounds := range [][2]int{{-1, 1}, {2, 1}, {0, 0}, {0, -1}, {0, MaxCallGraphPageSize + 1}, {int(^uint(0) >> 1), 1}} {
		if _, err := graph.CallsPage(bounds[0], bounds[1]); !errors.Is(err, ErrInvalidCallGraph) {
			t.Errorf("call bounds %v accepted: %v", bounds, err)
		}
		if _, err := graph.ScriptsPage(bounds[0], bounds[1]); !errors.Is(err, ErrInvalidCallGraph) {
			t.Errorf("script bounds %v accepted: %v", bounds, err)
		}
	}
	if calls, err := graph.CallsPage(1, 1); err != nil || len(calls) != 0 {
		t.Fatalf("end call page: %v, %v", calls, err)
	}
	if scripts, err := graph.ScriptsPage(1, 1); err != nil || len(scripts) != 0 {
		t.Fatalf("end script page: %v, %v", scripts, err)
	}
	if _, found := graph.Call("unknown"); found {
		t.Fatal("unknown call found")
	}
	if _, found := graph.Script("unknown"); found {
		t.Fatal("unknown script found")
	}
}

func TestZeroCallGraph(t *testing.T) {
	graph := CallGraph{}
	if graph.Valid() || graph.Complete() || graph.Entry() != (InvocationEntry{}) || graph.Budget() != (InvocationBudget{}) || graph.CallCount() != 0 || graph.ScriptCount() != 0 {
		t.Fatal("zero graph has facts")
	}
	if _, found := graph.Stop().Get(); found {
		t.Fatal("zero graph has stop")
	}
	if _, found := graph.EntryInterruption().Get(); found {
		t.Fatal("zero graph has interruption")
	}
	if _, found := graph.Call("unknown"); found {
		t.Fatal("zero graph has call")
	}
	if _, found := graph.Script("unknown"); found {
		t.Fatal("zero graph has script")
	}
	if !reflect.DeepEqual(graph.Snapshot(), CallGraphData{}) {
		t.Fatal("zero snapshot changed nil slices")
	}
	if _, err := graph.CallsPage(0, 1); !errors.Is(err, ErrInvalidCallGraph) {
		t.Fatal("zero graph accepted calls page")
	}
	if _, err := graph.ScriptsPage(0, 1); !errors.Is(err, ErrInvalidCallGraph) {
		t.Fatal("zero graph accepted scripts page")
	}
}
