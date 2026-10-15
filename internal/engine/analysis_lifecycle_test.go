package engine

import (
	"actionlint.kjanat.dev/internal/analyzerhost"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

type failAfterScheduling struct {
	RuleBase
	failure error
}

func (r *failAfterScheduling) VisitStep(*Step) error { return r.failure }

func TestAnalysisJoinsInjectedResourcesAfterVisitFailure(t *testing.T) {
	failure := errors.New("stop traversal")
	var completed atomic.Bool
	result, err := Analyze(t.Context(), AnalysisRequest{
		Sources: []SourceUnit{{Path: "workflow.yml", Content: []byte(commandGoodWorkflow)}},
		OnRulesCreated: func([]Rule) []Rule {
			checker := &scheduledResourceRule{RuleBase: NewRuleBase("resource", ""), completed: &completed}
			return []Rule{checker, &failAfterScheduling{RuleBase: NewRuleBase("fail", ""), failure: failure}}
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
	RuleBase
	workers   sync.WaitGroup
	completed *atomic.Bool
}

func (rule *scheduledResourceRule) attachScope(scope *analyzerhost.Scope) {
	scope.Register(func() error { rule.workers.Wait(); return nil })
}
func (rule *scheduledResourceRule) VisitStep(*Step) error {
	rule.workers.Go(func() { rule.completed.Store(true) })
	return nil
}
