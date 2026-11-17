package checking

import (
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/executor/composites"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis"
	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"
)

func TestSourceAnalyzerSurvivesRejectedInvocationIdentity(t *testing.T) {
	t.Parallel()
	content := "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: actions/checkout@v4}]}}\n"
	parsed := parsing.Workflow([]byte(content))
	entry := sourcetext.Reference{Document: "ci.yml", Revision: "revision-one", Node: "document"}
	builder, err := invocation.NewBuilder(invocation.EntryWorkflow, entry, invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	graph, err := builder.Publish()
	if err != nil {
		t.Fatal(err)
	}
	batch, err := scriptanalysis.NewBatch(graph, scriptanalysis.Collection{Scopes: []*scriptanalysis.Scope{{Context: builder.RootContext()}}})
	if err != nil {
		t.Fatal(err)
	}
	actual := entry
	actual.Revision = "current"
	input, err := composites.CheckInput(composites.Composition{Batch: batch}, "ci.yml", "ci.yml", []byte(content), parsed, map[string]facts.SourceIndex{"ci.yml": {Document: actual}})
	if !errors.Is(err, scriptanalysis.ErrInvalidBatch) {
		t.Fatalf("mismatched graph accepted: %v", err)
	}
	if input.Reference != actual || input.CallGraph.Valid() {
		t.Fatal("rejected graph reached analyzer input")
	}
	var used []rule.Descriptor
	findings, err := runDocumentAnalyzers(t.Context(), []checkinputs.Registration{{Analyzer: sourceOnlyProbe{content}}}, input, nil, &used)
	if err != nil || len(findings) != 1 || findings[0].Path != "ci.yml" || findings[0].Message != "reported source finding" {
		t.Fatalf("source-only fallback lost: %v %v", findings, err)
	}
}
