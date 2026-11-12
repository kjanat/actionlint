package expressions

import (
	"path/filepath"
	"testing"

	checkcalls "actionlint.kjanat.dev/v2/internal/checking/executor/calls"
	"actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture/declarations"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
)

type invocationMetadata struct{ metadata *actionmeta.Metadata }

func (m invocationMetadata) FindMetadata(string) (*actionmeta.Metadata, bool, error) {
	return m.metadata, true, nil
}

func TestRepeatedCompositeInvocationsUseTheirOwnOutputMetadata(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "outer/action.yml")
	content := []byte("name: outer\ndescription: outer\noutputs:\n  result:\n    description: result\n    value: ${{ steps.child.outputs.missing }}\nruns:\n  using: composite\n  steps:\n    - uses: $/child\n      id: child\n    - run: echo ready\n      shell: bash\n      env: {VALUE: '${{ steps.child.outputs.missing }}'}\n")
	_, _, registry := declarations.NewCapturedSnapshots(t, root)
	snapshot, err := registry.Document(path, content, parsing.Action(path, content))
	if err != nil {
		t.Fatal(err)
	}
	body, available := snapshot.ActionView()
	if !available {
		t.Fatal("composite body is unavailable")
	}
	builder, err := invocation.NewBuilder(invocation.EntryAction, snapshot.Reference(), invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	rule := New(t.Context(), nil, nil, nil, nil)
	parent := rule.Checker
	for _, closed := range []bool{true, false, true} {
		using := "runs: {using: node24, main: index.js}\n"
		if closed {
			using = "runs: {using: composite, steps: []}\n"
		}
		metadata, err := actionmeta.Parse("child/action.yml", []byte("name: child\ndescription: child\n"+using))
		if err != nil {
			t.Fatal(err)
		}
		mark := rule.FindingCount()
		context := builder.RootContext()
		if err := rule.EnterInvocation(checkcalls.Invocation{Context: context, Source: snapshot, Path: path, Actions: invocationMetadata{metadata}}); err != nil {
			t.Fatal(err)
		}
		for step := range body.Steps() {
			if err := rule.VisitInvocationStep(step); err != nil {
				t.Fatal(err)
			}
		}
		if err := rule.LeaveInvocation(context); err != nil {
			t.Fatal(err)
		}
		want := 0
		if closed {
			want = 2
		}
		if got := rule.FindingCount() - mark; got != want || rule.Checker != parent || len(rule.frames) != 0 {
			t.Fatalf("closed=%t: findings=%d, want %d; parent restored=%t; frames=%d", closed, got, want, rule.Checker == parent, len(rule.frames))
		}
	}
}
