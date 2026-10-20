package composition

import (
	"context"
	"errors"
	"testing"

	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

type phaseScriptAnalyzer struct {
	t       *testing.T
	visited *bool
	calls   int
	failure error
}

func (a *phaseScriptAnalyzer) AnalyzeScripts(_ context.Context, batch scriptexecution.Batch) ([]*findingmodel.Finding, error) {
	a.t.Helper()
	if !*a.visited || len(batch.Scripts) != 1 {
		a.t.Fatalf("scripts ran before traversal: visited=%v scripts=%d", *a.visited, len(batch.Scripts))
	}
	a.calls++
	return nil, a.failure
}

type phaseRule struct {
	workflowvisit.Base
	visited *bool
	failure error
	joined  *bool
}

func (r *phaseRule) VisitStep(*workflowlang.Step) error { *r.visited = true; return r.failure }
func TestCompositionCompletesScriptsAndResourcesAfterVisitFailure(t *testing.T) {
	for _, failVisit := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "visit failure"}[failVisit], func(t *testing.T) {
			parsed := documentparse.Workflow("workflow.yml", []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"))
			scriptFailure := errors.New("script failure")
			visitFailure := errors.New("visit failure")
			visited, joined := false, false
			pass := &phaseRule{Base: workflowvisit.NewBase("phase", ""), visited: &visited, joined: &joined}
			if failVisit {
				pass.failure = visitFailure
			}
			script := &phaseScriptAnalyzer{t: t, visited: &visited, failure: scriptFailure}
			service := New(Options{
				Context: t.Context(), Actions: actionlocal.NewResolver(nil, nil), Rules: []workflowvisit.Rule{pass},
				CollectScripts: true, Scripts: scriptexecution.Service{Analyzers: []scriptexecution.Analyzer{script}}, Join: func() error {
					joined = true
					pass.Error(&workflowlang.Pos{Line: 1, Col: 1}, "joined finding")
					return nil
				},
			})
			result, err := service.Workflow(parsed.Workflow)
			if !errors.Is(err, scriptFailure) || errors.Is(err, visitFailure) != failVisit || !joined || script.calls != 1 || len(result.Batch.Scripts) != 1 || len(result.Findings) != 1 || result.Findings[0].Message != "joined finding" {
				t.Fatalf("error=%v joined=%v calls=%d result=%+v", err, joined, script.calls, result)
			}
		})
	}
}
