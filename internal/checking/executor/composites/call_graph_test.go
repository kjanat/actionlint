package composites

import (
	"errors"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/rule"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"
)

func TestAnalyzerCallGraphIndependentOfScriptCollection(t *testing.T) {
	t.Parallel()
	for _, collect := range []bool{false, true} {
		f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo root}, {uses: $/child}]}}\n", map[string]string{
			"child/action.yml": "name: child\ndescription: child\nruns: {using: composite, steps: [{shell: bash, run: echo child}]}\n",
		}, nil)
		f.options.CollectScripts = collect
		result, err := Workflow(f.options, f.root)
		if err != nil {
			t.Fatal(err)
		}
		content := f.root.Content()
		input, err := CheckInput(result, "ci.yml", "ci.yml", content, parsing.Workflow(content), nil)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(input.AvailableInputs, rule.Invocations) || slices.Contains(input.AvailableInputs, rule.Scripts) != collect || !input.CallGraph.Valid() {
			t.Fatalf("fact availability disagrees with collection: %+v", input.AvailableInputs)
		}
		if input.CallGraph.CallCount() != 1 || input.CallGraph.ScriptCount() != 2 || input.CallGraph.Incomplete() {
			t.Fatalf("uncollected text removed graph facts: %+v", input.CallGraph.Calls())
		}
		if collect {
			if len(input.Scripts) != 1 || len(input.Invoked) != 1 || input.Scripts[0].Occurrence.Owner != input.CallGraph.RootID() || input.Invoked[0].Occurrence.Owner == input.CallGraph.RootID() {
				t.Fatal("script classification lost canonical owners")
			}
		} else if len(input.Scripts)+len(input.Invoked)+len(input.ActionInputs) != 0 {
			t.Fatal("uncollected text materialized")
		}
	}
}

func TestAnalyzerCallGraphRequiresMatchingDocumentIdentity(t *testing.T) {
	t.Parallel()
	f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo root}]}}\n", nil, nil)
	result, err := Workflow(f.options, f.root)
	if err != nil {
		t.Fatal(err)
	}
	content, reference := f.root.Content(), f.root.Reference()
	document := parsing.Workflow(content)
	for _, test := range []struct {
		name      string
		reference sourcetext.Reference
		valid     bool
	}{
		{"matching", reference, true},
		{"absent", sourcetext.Reference{}, true},
		{"foreign document", sourcetext.Reference{Document: "other.yml", Revision: reference.Revision, Node: reference.Node, Address: reference.Address}, false},
		{"stale revision", sourcetext.Reference{Document: reference.Document, Revision: "old", Node: reference.Node, Address: reference.Address}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			refs := map[string]facts.SourceIndex{"ci.yml": {Document: test.reference}}
			input, err := CheckInput(result, "ci.yml", "ci.yml", content, document, refs)
			if !test.valid {
				if !errors.Is(err, scriptanalysis.ErrInvalidBatch) {
					t.Fatalf("mismatched identity accepted: %v", err)
				}
				if input.Content != string(content) || input.Path != "ci.yml" || input.Reference != test.reference || len(input.AvailableInputs) != 1 || input.AvailableInputs[0] != rule.Source || input.CallGraph.Valid() {
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
	t.Parallel()
	content := []byte("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: actions/checkout@v4}]}}\n")
	f := newInvocationFixture(t, t.Context(), string(content), nil, nil)
	result, err := Workflow(f.options, f.root)
	if err != nil {
		t.Fatal(err)
	}
	input, err := CheckInput(result, "ci.yml", "ci.yml", content, parsing.Workflow(content), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(input.AvailableInputs, rule.Scripts) || !slices.Contains(input.AvailableInputs, rule.Invocations) || !input.CallGraph.Valid() || input.CallGraph.ScriptCount() != 0 {
		t.Fatal("empty collected graph was treated as unavailable")
	}
	input, err = CheckInput(Composition{}, "bad.yml", "bad.yml", nil, parsing.Document{}, nil)
	if err != nil || input.CallGraph.Valid() || slices.Contains(input.AvailableInputs, rule.Invocations) {
		t.Fatalf("unavailable graph: %+v %v", input, err)
	}
}
