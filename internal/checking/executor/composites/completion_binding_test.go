package composites

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/rulebinding"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

func TestIncompleteMetadataCheckUsesPreparedOccurrence(t *testing.T) {
	t.Parallel()
	rules, err := configuration.NewRuleSet([]rule.Declaration{{Name: "missing-file", Category: "correctness", Scope: rule.CompositeConfiguration}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		level string
		want  int
	}{{"off", 0}, {"error", 1}} {
		t.Run(test.level, func(t *testing.T) {
			t.Parallel()
			source, err := rules.ParseConfig([]byte("lint: {rules: {correctness: {missing-file: off}}}"))
			if err != nil {
				t.Fatal(err)
			}
			occurrence, err := rules.ParseConfig([]byte("lint: {rules: {correctness: {missing-file: " + test.level + "}}}"))
			if err != nil {
				t.Fatal(err)
			}
			registry := rulebinding.NewRegistry(rules)
			prepared := registry.Prepare(source, source, occurrence)
			fixture := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: []}}\n", nil, nil)
			graph, err := invocation.NewBuilder(invocation.EntryWorkflow, fixture.root.Reference(), invocation.DefaultBudget())
			if err != nil {
				t.Fatal(err)
			}
			analysis := compositeAnalyzer{bindings: prepared, compositionSession: &compositionSession{
				invocations: graph,
				passes:      []workflowvisit.Pass{&failingMetadataParticipant{Base: workflowvisit.NewBase("missing-file", "")}},
				bindSource: func(parent *rulebinding.Scope, _ string) (*rulebinding.Scope, error) {
					return registry.Prepare(parent.Caller(), source, parent.Composite()), nil
				},
			}}
			meta := &actionmeta.Metadata{Runs: actionmeta.Runs{Using: "node26", Main: "index.js"}}
			if err := analysis.recordUncheckedFiles(meta, actionmeta.ReferencedFiles{}, fixture.root.Reference(), graph.RootContext()); err != nil {
				t.Fatal(err)
			}
			if len(analysis.incompleteChecks) != test.want {
				t.Fatalf("occurrence %s produced incomplete checks %+v, want %d despite source-level off", test.level, analysis.incompleteChecks, test.want)
			}
		})
	}
}
