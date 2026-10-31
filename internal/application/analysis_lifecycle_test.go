package application

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

type failAfterScheduling struct {
	workflowvisit.Base
	failure error
}

func (r *failAfterScheduling) VisitStep(workflowdecl.StepView) error { return r.failure }

func TestAnalysisJoinsInjectedResourcesAfterVisitFailure(t *testing.T) {
	failure := errors.New("stop traversal")
	var completed atomic.Bool
	result, err := runTestAnalysis(t.Context(), analysisRequest{
		Sources: []sourceUnit{{Path: "workflow.yml", Content: []byte(commandGoodWorkflow)}},
		OnRulesCreated: func([]workflowvisit.Rule) []workflowvisit.Rule {
			checker := &scheduledResourceRule{Base: workflowvisit.NewBase("resource", ""), completed: &completed}
			return []workflowvisit.Rule{checker, &failAfterScheduling{Base: workflowvisit.NewBase("fail", ""), failure: failure}}
		},
	})
	if !errors.Is(err, failure) {
		t.Fatalf("error=%v result=%v", err, result)
	}
	if !completed.Load() {
		t.Fatal("analysis returned before the injected checker finished")
	}
}

type scheduledResourceRule struct {
	workflowvisit.Base
	workers   sync.WaitGroup
	completed *atomic.Bool
}

func (rule *scheduledResourceRule) attachScope(scope *analyzerhost.Scope) {
	scope.Register(func() error { rule.workers.Wait(); return nil })
}

func (rule *scheduledResourceRule) VisitStep(workflowdecl.StepView) error {
	rule.workers.Go(func() { rule.completed.Store(true) })
	return nil
}

type failingResourceRule struct {
	workflowvisit.Base
	failure error
}

func (r *failingResourceRule) attachScope(scope *analyzerhost.Scope) {
	scope.Register(func() error { return r.failure })
}

func TestAnalysisReportsResourceFailureOnce(t *testing.T) {
	failure := errors.New("resource completion failed")
	_, err := runTestAnalysis(t.Context(), analysisRequest{
		Sources: []sourceUnit{{Path: "workflow.yml", Content: []byte(commandGoodWorkflow)}},
		OnRulesCreated: func([]workflowvisit.Rule) []workflowvisit.Rule {
			return []workflowvisit.Rule{&failingResourceRule{Base: workflowvisit.NewBase("resource", ""), failure: failure}}
		},
	})
	if !errors.Is(err, failure) || strings.Count(err.Error(), failure.Error()) != 1 {
		t.Fatalf("completion error reported incorrectly: %v", err)
	}
}
