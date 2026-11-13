package checking

import (
	"errors"
	"testing"

	composites "actionlint.kjanat.dev/v2/internal/checking/executor/composites"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	scriptexecution "actionlint.kjanat.dev/v2/internal/checking/scripts"
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
)

func TestSourceAnalyzerSurvivesRejectedInvocationIdentity(t *testing.T) {
	t.Parallel()
	content := "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: actions/checkout@v4}]}}\n"
	parsed := parsing.Workflow([]byte(content))
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
	input, err := composites.CheckInput(composites.Result{Batch: batch}, "ci.yml", "ci.yml", []byte(content), parsed, map[string]workflowfacts.SourceIndex{"ci.yml": {Document: actual}})
	if !errors.Is(err, scriptexecution.ErrInvalidBatch) {
		t.Fatalf("mismatched graph accepted: %v", err)
	}
	if input.Reference != actual || input.CallGraph.Valid() {
		t.Fatal("rejected graph reached analyzer input")
	}
	var used []rule.Descriptor
	findings, err := runDocumentAnalyzers(t.Context(), []checkinputs.Registration{{Checker: sourceOnlyProbe{content}}}, input, nil, &used)
	if err != nil || len(findings) != 1 || findings[0].Path != "ci.yml" || findings[0].Message != "reported source finding" {
		t.Fatalf("source-only fallback lost: %v %v", findings, err)
	}
}
