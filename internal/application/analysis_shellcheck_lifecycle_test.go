package application

import (
	"errors"
	"sync/atomic"
	"testing"

	"actionlint.kjanat.dev/internal/tools/adapters"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
)

func TestShellcheckPipelineCompletesAfterTraversalFailure(t *testing.T) {
	failure := errors.New("stop traversal")
	var completed atomic.Bool
	result, err := runTestAnalysis(t.Context(), analysisRequest{
		Sources: []sourceUnit{{Path: "workflow.yml", Content: []byte(commandGoodWorkflow)}},

		OnRulesCreated: func([]workflowvisit.Rule) []workflowvisit.Rule {
			return []workflowvisit.Rule{&failAfterScheduling{Base: workflowvisit.NewBase("fail", ""), failure: failure}}
		}, Tools: (adapters.Options{ShellcheckRunner: func([]string, string) ([]byte, error) {
			completed.Store(true)
			return []byte(`{"comments":[{"line":3,"endLine":3,"column":1,"endColumn":2,"code":9999,"level":"warning","message":"retained finding"}]}`), nil
		}}).Registrations(),
	})
	if !errors.Is(err, failure) || !completed.Load() || result == nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "shellcheck" {
		t.Fatalf("completion or partial findings lost: result=%+v err=%v completed=%v", result, err, completed.Load())
	}
}
