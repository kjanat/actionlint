package engine

import (
	"errors"
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
			checker := NewRuleShellcheckWithRunner(func([]string, string) ([]byte, error) { completed.Store(true); return []byte(`[]`), nil })
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
