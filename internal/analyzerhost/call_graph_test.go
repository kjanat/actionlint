package analyzerhost

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/source"
)

func TestRunIsolatesCallGraphAndOccurrenceValues(t *testing.T) {
	root, call, script := analyzer.OccurrenceID(strings.Repeat("1", 64)), analyzer.OccurrenceID(strings.Repeat("2", 64)), analyzer.OccurrenceID(strings.Repeat("3", 64))
	ref := source.Reference{Document: "entry", Node: "document"}
	target := source.Reference{Document: "child", Node: "document"}
	outcome, err := analyzer.NewCallOutcome(analyzer.InterruptedCall{Reason: analyzer.Canceled, Target: analyzer.Present(target)})
	if err != nil {
		t.Fatal(err)
	}
	records := analyzer.CallGraphData{
		Entry:   analyzer.InvocationEntry{ID: root, Kind: analyzer.WorkflowEntry, Reference: ref},
		Budget:  analyzer.InvocationBudget{MaxDepth: 2, MaxCalls: 2, MaxScripts: 1},
		Calls:   []analyzer.Call{{ID: call, Parent: root, Ancestry: []analyzer.OccurrenceID{root}, Depth: 1, Site: source.Reference{Document: "entry", Node: "call"}, Condition: analyzer.CallEnabled, Outcome: outcome}},
		Scripts: []analyzer.ScriptOccurrence{{ID: script, Owner: call, Ancestry: []analyzer.OccurrenceID{root, call}, Depth: 1, Declaration: source.Reference{Document: "child", Node: "run"}, Kind: analyzer.RunScript}},
		Stop:    analyzer.Present(analyzer.InvocationStop{Parent: call, Site: source.Reference{Document: "child", Node: "later"}, Limit: analyzer.ScriptLimit, Max: 1}),
	}
	graph, err := analyzer.NewCallGraph(records)
	if err != nil {
		t.Fatal(err)
	}
	input := analyzer.Input{Reference: ref, CallGraph: graph, Available: []analyzer.Fact{analyzer.Invocations, analyzer.Scripts}, Invoked: []analyzer.Script{{Reference: records.Scripts[0].Declaration, Occurrence: records.Scripts[0]}}}
	input.SelectedScripts = []analyzer.OccurrenceID{script}
	want := graph.Snapshot()
	changed := make(chan struct{})
	first := testAnalyzer{analyzer.Descriptor{Name: "mutator"}, func(_ context.Context, in analyzer.Input) ([]diagnostic.Diagnostic, error) {
		in.SelectedScripts[0] = "changed"
		snapshot := in.CallGraph.Snapshot()
		snapshot.Calls[0].Ancestry[0] = "changed"
		if snapshot.Scripts[0].Ancestry[0] != root {
			t.Error("record ancestry storage overlaps")
		}
		snapshot.Scripts[0].Ancestry[1] = "changed"
		in.Invoked[0].Occurrence.Ancestry[0] = "changed"
		payload := snapshot.Calls[0].Outcome.Value().(analyzer.InterruptedCall)
		copyTarget, _ := payload.Target.Get()
		copyTarget.Node = "changed"
		payload.Target = analyzer.Present(copyTarget)
		snapshot.Calls[0].Outcome, _ = analyzer.NewCallOutcome(payload)
		snapshot.Calls = append(snapshot.Calls, analyzer.Call{})
		snapshot.Stop = analyzer.Optional[analyzer.InvocationStop]{}
		snapshot.EntryInterruption = analyzer.Present(analyzer.Interruption{Reason: analyzer.OperationalFailure})
		in.CallGraph = analyzer.CallGraph{}
		close(changed)
		return nil, nil
	}}
	second := testAnalyzer{analyzer.Descriptor{Name: "observer"}, func(_ context.Context, in analyzer.Input) ([]diagnostic.Diagnostic, error) {
		<-changed
		if len(in.SelectedScripts) != 1 || in.SelectedScripts[0] != script {
			t.Error("analyzer mutation reached sibling selection")
		}
		if !reflect.DeepEqual(in.CallGraph.Snapshot(), want) || !reflect.DeepEqual(in.Invoked[0].Occurrence, want.Scripts[0]) {
			t.Error("analyzer mutation reached sibling input")
		}
		return nil, nil
	}}
	if _, err := Run(t.Context(), input, []analyzer.Registration{{Analyzer: first}, {Analyzer: second}}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(graph.Snapshot(), want) || input.SelectedScripts[0] != script {
		t.Fatal("analyzer mutation changed published graph")
	}
}
