package scripts

import (
	"context"
	"slices"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	workflow "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

type rangeReviewBackend struct{ end checkinputs.ScriptPosition }

func (b rangeReviewBackend) Check(context.Context, checkinputs.ScriptRequest) ([]checkinputs.ScriptDiagnostic, error) {
	return []checkinputs.ScriptDiagnostic{
		{Message: "valid", Location: checkinputs.ScriptPosition{Row: 1, Column: 1}, EndLocation: checkinputs.ScriptPosition{Row: 1, Column: 2}},
		{Message: "reversed", Location: checkinputs.ScriptPosition{Row: 2, Column: 5}, EndLocation: b.end},
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
	run, ok := parsed.Jobs["test"].Steps[0].Exec.(*workflow.ExecRun)
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
	for _, end := range []checkinputs.ScriptPosition{{Row: 2, Column: 3}, {Row: 1, Column: 7}} {
		registration, err := Register(rule.Descriptor{Name: "range-check"}, []string{"bash"}, rangeReviewBackend{end})
		if err != nil {
			t.Fatal(err)
		}
		checker, err := registration.Build(tools.Context{})
		if err != nil {
			t.Fatal(err)
		}
		findings, err := checker.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{{Text: run.Run.Value, Source: ownedRun.Source(), Key: run.RunPos, Path: "ci.yml", Content: content, Execution: tools.Execution{Shell: "bash"}}}})
		if err != nil || len(findings) != 2 || findings[0].Message != "valid" || findings[1].Message != "reversed" ||
			findings[1].Start != (sourcepos.Position{Line: 8, Column: 15}) || findings[1].End != (sourcepos.Position{}) {
			t.Fatalf("reversed end kept or finding lost: findings=%+v err=%v", findings, err)
		}
	}
}
