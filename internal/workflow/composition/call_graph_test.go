package composition

import (
	"errors"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"
	workflowfacts "actionlint.kjanat.dev/v2/internal/workflow/facts"
	"actionlint.kjanat.dev/v2/source"
)

func TestAnalyzerCallGraphIndependentOfScriptCollection(t *testing.T) {
	for _, collect := range []bool{false, true} {
		f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo root}, {uses: $/child}]}}\n", map[string]string{
			"child/action.yml": "name: child\ndescription: child\nruns: {using: composite, steps: [{shell: bash, run: echo child}]}\n",
		}, nil)
		f.service.options.CollectScripts = collect
		result, err := f.service.Workflow(f.root)
		if err != nil {
			t.Fatal(err)
		}
		content := f.root.Content()
		input, err := result.AnalyzerInput("ci.yml", "ci.yml", content, documentparse.Workflow("ci.yml", content), nil)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(input.Available, analyzer.Invocations) || slices.Contains(input.Available, analyzer.Scripts) != collect || !input.CallGraph.Valid() {
			t.Fatalf("fact availability disagrees with collection: %+v", input.Available)
		}
		graph := input.CallGraph.Snapshot()
		if input.CallGraph.CallCount() != 1 || input.CallGraph.ScriptCount() != 2 || !input.CallGraph.Complete() {
			t.Fatalf("uncollected text removed graph facts: %+v", graph)
		}
		if collect {
			if len(input.Scripts) != 1 || len(input.Invoked) != 1 || input.Scripts[0].Occurrence.Owner != input.CallGraph.Entry().ID || input.Invoked[0].Occurrence.Owner == input.CallGraph.Entry().ID {
				t.Fatal("script classification lost canonical owners")
			}
		} else if len(input.Scripts)+len(input.Invoked)+len(input.ActionInputs) != 0 {
			t.Fatal("uncollected text materialized")
		}
	}
}

func TestAnalyzerCallGraphRequiresMatchingDocumentIdentity(t *testing.T) {
	f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo root}]}}\n", nil, nil)
	result, err := f.service.Workflow(f.root)
	if err != nil {
		t.Fatal(err)
	}
	content, reference := f.root.Content(), f.root.Reference()
	document := documentparse.Workflow("ci.yml", content)
	for _, test := range []struct {
		name      string
		reference source.Reference
		valid     bool
	}{
		{"matching", reference, true},
		{"absent", source.Reference{}, true},
		{"foreign document", source.Reference{Document: "other.yml", Revision: reference.Revision, Node: reference.Node, Address: reference.Address}, false},
		{"stale revision", source.Reference{Document: reference.Document, Revision: "old", Node: reference.Node, Address: reference.Address}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			refs := map[string]workflowfacts.SourceIndex{"ci.yml": {Document: test.reference}}
			input, err := result.AnalyzerInput("ci.yml", "ci.yml", content, document, refs)
			if !test.valid {
				if !errors.Is(err, scriptexecution.ErrInvalidBatch) {
					t.Fatalf("mismatched identity accepted: %v", err)
				}
				if input.Content != string(content) || input.Path != "ci.yml" || input.Reference != test.reference || len(input.Available) != 1 || input.Available[0] != analyzer.Source || input.CallGraph.Valid() {
					t.Fatalf("invalid graph did not retain source-only input: %+v", input)
				}
				return
			}
			if err != nil || input.Reference != reference {
				t.Fatalf("document identity lost: %+v %v", input.Reference, err)
			}
			if err := input.ValidateInvocations(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAnalyzerEmptyCollectedGraphAndUnavailableGraph(t *testing.T) {
	content := []byte("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: actions/checkout@v4}]}}\n")
	f := newInvocationFixture(t, t.Context(), string(content), nil, nil)
	result, err := f.service.Workflow(f.root)
	if err != nil {
		t.Fatal(err)
	}
	input, err := result.AnalyzerInput("ci.yml", "ci.yml", content, documentparse.Workflow("ci.yml", content), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(input.Available, analyzer.Scripts) || !slices.Contains(input.Available, analyzer.Invocations) || !input.CallGraph.Valid() || input.CallGraph.ScriptCount() != 0 {
		t.Fatal("empty collected graph was treated as unavailable")
	}
	input, err = (Result{}).AnalyzerInput("bad.yml", "bad.yml", nil, documentparse.Document{}, nil)
	if err != nil || input.CallGraph.Valid() || slices.Contains(input.Available, analyzer.Invocations) {
		t.Fatalf("unavailable graph: %+v %v", input, err)
	}
}
