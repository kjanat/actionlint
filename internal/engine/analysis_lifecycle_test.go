package engine

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"actionlint.kjanat.dev/internal/analyzerhost"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
)

type failAfterScheduling struct {
	workflowvisit.Base
	failure error
}

func (r *failAfterScheduling) VisitStep(*workflowlang.Step) error { return r.failure }

func TestAnalysisJoinsInjectedResourcesAfterVisitFailure(t *testing.T) {
	failure := errors.New("stop traversal")
	var completed atomic.Bool
	result, err := runTestAnalysis(t.Context(), Request{
		Sources: []SourceUnit{{Path: "workflow.yml", Content: []byte(commandGoodWorkflow)}},
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

func (rule *scheduledResourceRule) VisitStep(*workflowlang.Step) error {
	rule.workers.Go(func() { rule.completed.Store(true) })
	return nil
}
