package composition

import (
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/documentparse"
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
		if !slices.Contains(input.Available, analyzer.Invocations) || slices.Contains(input.Available, analyzer.Scripts) != collect || input.CallGraph == nil {
			t.Fatalf("fact availability disagrees with collection: %+v", input.Available)
		}
		if err := input.CallGraph.Validate(); err != nil {
			t.Fatal(err)
		}
		if len(input.CallGraph.Calls) != 1 || len(input.CallGraph.Scripts) != 2 || !input.CallGraph.Complete {
			t.Fatalf("uncollected text removed graph facts: %+v", input.CallGraph)
		}
		if collect {
			if len(input.Scripts) != 1 || len(input.Invoked) != 1 || input.Scripts[0].Occurrence.Owner != input.CallGraph.Entry.ID || input.Invoked[0].Occurrence.Owner == input.CallGraph.Entry.ID {
				t.Fatal("script classification lost canonical owners")
			}
		} else if len(input.Scripts)+len(input.Invoked)+len(input.ActionInputs) != 0 {
			t.Fatal("uncollected text materialized")
		}
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
	if !slices.Contains(input.Available, analyzer.Scripts) || !slices.Contains(input.Available, analyzer.Invocations) || input.CallGraph == nil || len(input.CallGraph.Scripts) != 0 {
		t.Fatal("empty collected graph was treated as unavailable")
	}
	input, err = (Result{}).AnalyzerInput("bad.yml", "bad.yml", nil, documentparse.Document{}, nil)
	if err != nil || input.CallGraph != nil || slices.Contains(input.Available, analyzer.Invocations) {
		t.Fatalf("unavailable graph: %+v %v", input, err)
	}
}
