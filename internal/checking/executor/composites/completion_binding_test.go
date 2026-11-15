package composites

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/binding"
	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"
	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
)

func TestIncompleteMetadataCheckUsesPreparedOccurrence(t *testing.T) {
	t.Parallel()
	rules, err := configuration.NewRuleSet([]rulemeta.Declaration{{Name: "metadata-files", Category: "correctness", Scope: rulemeta.CompositeConfiguration}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		level string
		want  int
	}{{"off", 0}, {"error", 1}} {
		t.Run(test.level, func(t *testing.T) {
			t.Parallel()
			source, err := rules.Parse([]byte("lint: {rules: {correctness: {metadata-files: off}}}"))
			if err != nil {
				t.Fatal(err)
			}
			occurrence, err := rules.Parse([]byte("lint: {rules: {correctness: {metadata-files: " + test.level + "}}}"))
			if err != nil {
				t.Fatal(err)
			}
			registry := binding.NewRegistry(rules)
			prepared := registry.Prepare(source, source, occurrence)
			fixture := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: []}}\n", nil, nil)
			graph, err := invocation.NewBuilder(invocation.EntryWorkflow, fixture.root.Reference(), invocation.DefaultBudget())
			if err != nil {
				t.Fatal(err)
			}
			analysis := compositeAnalyzer{bindings: prepared, compositionSession: &compositionSession{
				invocations: graph,
				passes:      []workflowvisit.Pass{&failingMetadataParticipant{Base: workflowvisit.NewBase("metadata-files", "")}},
				bindSource: func(parent *binding.Scope, _ string) (*binding.Scope, error) {
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
