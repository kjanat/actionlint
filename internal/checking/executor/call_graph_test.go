package executor

import (
	"context"
	"reflect"
	"strings"
	"testing"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestRunIsolatesCallGraphAndOccurrenceValues(t *testing.T) {
	t.Parallel()
	root, call, script := checkinputs.OccurrenceID(strings.Repeat("1", 64)), checkinputs.OccurrenceID(strings.Repeat("2", 64)), checkinputs.OccurrenceID(strings.Repeat("3", 64))
	ref := source.Reference{Document: "entry", Node: "document"}
	target := source.Reference{Document: "child", Node: "document"}
	outcome, err := checkinputs.NewCallOutcome(checkinputs.InterruptedCall{Reason: checkinputs.Canceled, Target: checkinputs.Present(target)})
	if err != nil {
		t.Fatal(err)
	}
	records := checkinputs.CallGraphData{
		Entry:   checkinputs.InvocationEntry{ID: root, Kind: checkinputs.WorkflowEntry, Reference: ref},
		Budget:  checkinputs.InvocationBudget{MaxDepth: 2, MaxCalls: 2, MaxScripts: 1},
		Calls:   []checkinputs.Call{{ID: call, Parent: root, Ancestry: []checkinputs.OccurrenceID{root}, Depth: 1, Site: source.Reference{Document: "entry", Node: "call"}, Condition: checkinputs.CallEnabled, Outcome: outcome}},
		Scripts: []checkinputs.ScriptOccurrence{{ID: script, Owner: call, Ancestry: []checkinputs.OccurrenceID{root, call}, Depth: 1, Declaration: source.Reference{Document: "child", Node: "run"}, Kind: checkinputs.RunScript}},
		Stop:    checkinputs.Present(checkinputs.InvocationStop{Parent: call, Site: source.Reference{Document: "child", Node: "later"}, Limit: checkinputs.ScriptLimit, Max: 1}),
	}
	graph, err := checkinputs.NewCallGraph(records)
	if err != nil {
		t.Fatal(err)
	}
	input := checkinputs.Input{Reference: ref, CallGraph: graph, AvailableInputs: []rulemeta.InputKind{rulemeta.Invocations, rulemeta.Scripts}, Invoked: []checkinputs.Script{{Reference: records.Scripts[0].Declaration, Occurrence: records.Scripts[0]}}}
	input.SelectedScripts = []checkinputs.OccurrenceID{script}
	want := graph.Export()
	changed := make(chan struct{})
	first := testAnalyzer{checkinputs.Descriptor{Name: "mutator"}, func(_ context.Context, in checkinputs.Input) ([]results.Diagnostic, error) {
		in.SelectedScripts[0] = "changed"
		snapshot := in.CallGraph.Export()
		snapshot.Calls[0].Ancestry[0] = "changed"
		if snapshot.Scripts[0].Ancestry[0] != root {
			t.Error("record ancestry storage overlaps")
		}
		snapshot.Scripts[0].Ancestry[1] = "changed"
		in.Invoked[0].Occurrence.Ancestry[0] = "changed"
		payload := snapshot.Calls[0].Outcome.Value().(checkinputs.InterruptedCall)
		copyTarget, _ := payload.Target.Get()
		copyTarget.Node = "changed"
		payload.Target = checkinputs.Present(copyTarget)
		snapshot.Calls[0].Outcome, _ = checkinputs.NewCallOutcome(payload)
		snapshot.Calls = append(snapshot.Calls, checkinputs.Call{})
		snapshot.Stop = checkinputs.Optional[checkinputs.InvocationStop]{}
		snapshot.EntryInterruption = checkinputs.Present(checkinputs.Interruption{Reason: checkinputs.OperationalFailure})
		in.CallGraph = checkinputs.CallGraph{}
		close(changed)
		return nil, nil
	}}
	second := testAnalyzer{checkinputs.Descriptor{Name: "observer"}, func(_ context.Context, in checkinputs.Input) ([]results.Diagnostic, error) {
		<-changed
		if len(in.SelectedScripts) != 1 || in.SelectedScripts[0] != script {
			t.Error("analyzer mutation reached sibling selection")
		}
		if !reflect.DeepEqual(in.CallGraph.Export(), want) || !reflect.DeepEqual(in.Invoked[0].Occurrence, want.Scripts[0]) {
			t.Error("analyzer mutation reached sibling input")
		}
		return nil, nil
	}}
	if _, err := Run(t.Context(), input, []checkinputs.Registration{{Checker: first}, {Checker: second}}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(graph.Export(), want) || input.SelectedScripts[0] != script {
		t.Fatal("analyzer mutation changed published graph")
	}
}
