package backend

import (
	"context"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/tools"
	workflow "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/rule"
)

type rangeReviewBackend struct{ end analyzer.ScriptPosition }

func (b rangeReviewBackend) Check(context.Context, analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	return []analyzer.ScriptDiagnostic{
		{Message: "valid", Location: analyzer.ScriptPosition{Row: 1, Column: 1}, EndLocation: analyzer.ScriptPosition{Row: 1, Column: 2}},
		{Message: "reversed", Location: analyzer.ScriptPosition{Row: 2, Column: 5}, EndLocation: b.end},
	}, nil
}

// A reversed end is dropped and its finding kept at the mapped start, as an
// unmapped end is; neither fails the check.
func TestBackendDropsReversedEnds(t *testing.T) {
	content := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: |\n          echo first\n          echo second\n")
	document := documentparse.Workflow("ci.yml", content)
	parsed, failures := document.Workflow, document.Findings
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
	for _, end := range []analyzer.ScriptPosition{{Row: 2, Column: 3}, {Row: 1, Column: 7}} {
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
			findings[1].Start != (diagnostic.Position{Line: 8, Column: 15}) || findings[1].End != (diagnostic.Position{}) {
			t.Fatalf("reversed end kept or finding lost: findings=%+v err=%v", findings, err)
		}
	}
}
