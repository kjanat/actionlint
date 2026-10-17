package engine

import (
	"errors"
	"sync/atomic"
	"testing"
)

func TestShellcheckPipelineCompletesAfterTraversalFailure(t *testing.T) {
	failure := errors.New("stop traversal")
	var completed atomic.Bool
	result, err := Analyze(t.Context(), AnalysisRequest{
		Sources: []SourceUnit{{Path: "workflow.yml", Content: []byte(commandGoodWorkflow)}},
		ShellcheckRunner: func([]string, string) ([]byte, error) {
			completed.Store(true)
			return []byte(`{"comments":[{"line":3,"endLine":3,"column":1,"endColumn":2,"code":9999,"level":"warning","message":"retained finding"}]}`), nil
		},
		OnRulesCreated: func([]Rule) []Rule {
			return []Rule{&failAfterScheduling{RuleBase: NewRuleBase("fail", ""), failure: failure}}
		},
	})
	if !errors.Is(err, failure) || !completed.Load() || result == nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "shellcheck" {
		t.Fatalf("completion or partial findings lost: result=%+v err=%v completed=%v", result, err, completed.Load())
	}
}
