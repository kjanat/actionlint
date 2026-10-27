package application

import (
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/composition"
	workflowfacts "actionlint.kjanat.dev/v2/internal/workflow/facts"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	"actionlint.kjanat.dev/v2/rule"
	"actionlint.kjanat.dev/v2/source"
)

func TestSourceAnalyzerSurvivesRejectedInvocationIdentity(t *testing.T) {
	content := "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: actions/checkout@v4}]}}\n"
	parsed := documentparse.Workflow("ci.yml", []byte(content))
	entry := source.Reference{Document: "ci.yml", Revision: "revision-one", Node: "document"}
	builder, err := invocation.NewBuilder(invocation.EntryWorkflow, entry, invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	graph, err := builder.Publish()
	if err != nil {
		t.Fatal(err)
	}
	batch, err := scriptexecution.NewBatch(graph, scriptexecution.Collection{Scopes: []*scriptexecution.Scope{{Context: builder.RootContext()}}})
	if err != nil {
		t.Fatal(err)
	}
	actual := entry
	actual.Revision = "current"
	input, err := (composition.Result{Batch: batch}).AnalyzerInput("ci.yml", "ci.yml", []byte(content), parsed, map[string]workflowfacts.SourceIndex{"ci.yml": {Document: actual}})
	if !errors.Is(err, scriptexecution.ErrInvalidBatch) {
		t.Fatalf("mismatched graph accepted: %v", err)
	}
	if input.Reference != actual || input.CallGraph.Valid() {
		t.Fatal("rejected graph reached analyzer input")
	}
	var used []rule.Descriptor
	findings, err := runDocumentAnalyzers(t.Context(), []analyzer.Registration{{Analyzer: sourceOnlyProbe{content}}}, input, nil, &used)
	if err != nil || len(findings) != 1 || findings[0].Path != "ci.yml" || findings[0].Message != "reported source finding" {
		t.Fatalf("source-only fallback lost: %v %v", findings, err)
	}
}
