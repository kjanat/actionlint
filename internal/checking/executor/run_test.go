package executor

import (
	"context"
	"errors"
	"strings"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/input/source"
	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/results"
)

type testAnalyzer struct {
	descriptor checkinputs.Descriptor
	run        func(context.Context, checkinputs.Input) ([]results.Diagnostic, error)
}

func (a testAnalyzer) Descriptor() checkinputs.Descriptor { return a.descriptor }
func (a testAnalyzer) CheckDocument(ctx context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	return a.run(ctx, input)
}

func TestRunIsolatesFactsAndRetainsPartialResults(t *testing.T) {
	failure := errors.New("incomplete")
	input := checkinputs.Input{Path: "workflow.yml", AvailableInputs: []checkinputs.InputKind{checkinputs.Source, checkinputs.Mappings}, Mappings: []workflowfacts.Mapping{{Entries: []workflowfacts.Entry{{Name: "original"}}}}}
	changed := make(chan struct{})
	input.SourceRoots = []checkinputs.SourceRoot{{Host: "/source", Guest: "/workspace"}}
	first := testAnalyzer{checkinputs.Descriptor{Name: "first", RequiredInputs: []checkinputs.InputKind{checkinputs.Mappings}}, func(_ context.Context, in checkinputs.Input) ([]results.Diagnostic, error) {
		in.Mappings[0].Entries[0].Name = "changed"
		in.SourceRoots[0].Guest = "/changed"
		close(changed)
		return []results.Diagnostic{{Message: "partial"}}, failure
	}}
	second := testAnalyzer{checkinputs.Descriptor{Name: "second", RequiredInputs: []checkinputs.InputKind{checkinputs.Mappings}}, func(_ context.Context, in checkinputs.Input) ([]results.Diagnostic, error) {
		<-changed
		if in.Mappings[0].Entries[0].Name != "original" {
			t.Error("shared mutable facts")
		}
		if in.SourceRoots[0].Guest != "/workspace" {
			t.Error("shared mutable source roots")
		}
		return []results.Diagnostic{{Message: "complete"}}, nil
	}}
	findings, err := Run(t.Context(), input, []checkinputs.Registration{{Checker: first}, {Checker: second}})
	if !errors.Is(err, failure) || len(findings) != 2 {
		t.Fatalf("findings=%v err=%v", findings, err)
	}
	if findings[0].Rule != "first" || findings[1].Rule != "second" || findings[0].Path != input.Path {
		t.Fatal(findings)
	}
	if input.Mappings[0].Entries[0].Name != "original" {
		t.Fatal("input modified")
	}
}

func TestRunRequirementsAndCancellation(t *testing.T) {
	for _, requirement := range []checkinputs.InputKind{checkinputs.Scripts, checkinputs.Invocations, checkinputs.Execution, "future"} {
		a := testAnalyzer{checkinputs.Descriptor{Name: "requires", RequiredInputs: []checkinputs.InputKind{requirement}}, func(context.Context, checkinputs.Input) ([]results.Diagnostic, error) {
			t.Error("ran without required facts")
			return nil, nil
		}}
		_, err := Run(t.Context(), checkinputs.Input{}, []checkinputs.Registration{{Checker: a}})
		if err == nil || !strings.Contains(err.Error(), string(requirement)) {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := Run(ctx, checkinputs.Input{}, []checkinputs.Registration{{}})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestRunIsolatesActionInputFacts(t *testing.T) {
	root, callID, scriptID := checkinputs.OccurrenceID(strings.Repeat("1", 64)), checkinputs.OccurrenceID(strings.Repeat("2", 64)), checkinputs.OccurrenceID(strings.Repeat("3", 64))
	declaration := source.Reference{Document: "action.yml", Revision: "r", Node: "script"}
	outcome, err := checkinputs.NewCallOutcome(checkinputs.ExpandedCall{Target: source.Reference{Document: "action.yml", Revision: "r", Node: "document"}})
	if err != nil {
		t.Fatal(err)
	}
	graph, err := checkinputs.NewCallGraph(checkinputs.CallGraphData{
		Entry:  checkinputs.InvocationEntry{ID: root, Kind: checkinputs.WorkflowEntry, Reference: source.Reference{Document: "workflow.yml", Revision: "r", Node: "document"}},
		Budget: checkinputs.InvocationBudget{MaxDepth: 1, MaxCalls: 1, MaxScripts: 1}, Complete: true,
		Calls:   []checkinputs.Call{{ID: callID, Parent: root, Ancestry: []checkinputs.OccurrenceID{root}, Depth: 1, Site: source.Reference{Document: "workflow.yml", Revision: "r", Node: "call"}, Uses: "owner/action", HasUses: true, Condition: checkinputs.CallEnabled, Outcome: outcome}},
		Scripts: []checkinputs.ScriptOccurrence{{ID: scriptID, Owner: callID, Ancestry: []checkinputs.OccurrenceID{root, callID}, Depth: 1, Declaration: declaration, Kind: checkinputs.ActionInputScript, Input: "script"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	occurrence, exists := graph.Script(scriptID)
	if !exists {
		t.Fatal("action input occurrence missing")
	}
	input := checkinputs.Input{Reference: graph.Entry().Reference, AvailableInputs: []checkinputs.InputKind{checkinputs.Scripts, checkinputs.Invocations}, CallGraph: graph, ActionInputs: []checkinputs.ActionInput{{
		Reference: declaration,
		Spans:     []scriptmodel.Span{{Line: 1, Start: 1, End: 8}}, Occurrence: occurrence,
		Site: checkinputs.ScriptInput{Action: "actions/github-script", Input: "script", Language: "javascript"},
	}}}
	changed := make(chan struct{})
	first := testAnalyzer{checkinputs.Descriptor{Name: "first", RequiredInputs: []checkinputs.InputKind{checkinputs.Scripts}}, func(_ context.Context, in checkinputs.Input) ([]results.Diagnostic, error) {
		in.ActionInputs[0].Site.Language = "python"
		in.ActionInputs[0].Spans[0].End = 99
		in.ActionInputs[0].Occurrence.Ancestry[0] = "changed"
		close(changed)
		return nil, nil
	}}
	second := testAnalyzer{checkinputs.Descriptor{Name: "second", RequiredInputs: []checkinputs.InputKind{checkinputs.Scripts}}, func(_ context.Context, in checkinputs.Input) ([]results.Diagnostic, error) {
		<-changed
		got := in.ActionInputs[0]
		if got.Site.Language != "javascript" || got.Spans[0].End != 8 || got.Occurrence.Ancestry[0] != root {
			t.Errorf("action input facts shared between analyzers: %+v", got)
		}
		return nil, nil
	}}
	if _, err := Run(t.Context(), input, []checkinputs.Registration{{Checker: first}, {Checker: second}}); err != nil {
		t.Fatal(err)
	}
}
