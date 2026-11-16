package backends

import (
	"context"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"actionlint.kjanat.dev/v2/internal/model/scripts"

	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/contract"
	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

type rangeReviewBackend struct{ end sourcetext.Position }

func (b rangeReviewBackend) Check(context.Context, scriptengine.Request) ([]scriptengine.Diagnostic, error) {
	return []scriptengine.Diagnostic{
		{Message: "valid", Location: sourcetext.Position{Line: 1, Column: 1}, EndLocation: sourcetext.Position{Line: 1, Column: 2}},
		{Message: "reversed", Location: sourcetext.Position{Line: 2, Column: 5}, EndLocation: b.end},
	}, nil
}

// A reversed end is dropped and its finding kept at the mapped start, as an
// unmapped end is; neither fails the check.
func TestBackendDropsReversedEnds(t *testing.T) {
	t.Parallel()
	content := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: |\n          echo first\n          echo second\n")
	document := parsing.Workflow(content)
	parsed, failures := document.Workflow, document.SyntaxErrors
	if len(failures) != 0 {
		t.Fatal(failures)
	}
	run, ok := parsed.Jobs["test"].Steps[0].Exec.(*definition.ExecRun)
	if !ok {
		t.Fatal("fixture is not a run step")
	}
	registry := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := registry.Document("fixture.yml", content, document)
	if err != nil {
		t.Fatal(err)
	}
	workflow, _ := snapshot.WorkflowView()
	job, _ := workflow.Job("test")
	stepView := slices.Collect(job.Steps())[0]
	ownedRun, _ := stepView.Run()
	scriptRun := *run
	scriptRun.Source = ownedRun.Source
	for _, end := range []sourcetext.Position{{Line: 2, Column: 3}, {Line: 1, Column: 7}} {
		registration, err := Register(rule.Descriptor{Name: "range-check"}, []string{"bash"}, rangeReviewBackend{end})
		if err != nil {
			t.Fatal(err)
		}
		checker, err := registration.Build(contract.Context{})
		if err != nil {
			t.Fatal(err)
		}
		findings, err := checker.Analyze(t.Context(), scriptanalysis.Batch{Scripts: []scriptanalysis.Unit{{Run: &scriptRun, Path: "ci.yml", Content: content, Execution: &scripts.Execution{Shell: scripts.Shell{Name: "bash"}}}}})
		if err != nil || len(findings) != 2 || findings[0].Message != "valid" || findings[1].Message != "reversed" ||
			findings[1].Start != (sourcetext.Position{Line: 8, Column: 15}) || findings[1].End != (sourcetext.Position{}) {
			t.Fatalf("reversed end kept or finding lost: findings=%+v err=%v", findings, err)
		}
	}
}
