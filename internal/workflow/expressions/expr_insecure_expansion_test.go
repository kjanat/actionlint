package expressions

import (
	"testing"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

func TestAllScriptInterpolationsChecked(t *testing.T) {
	rule := newTestChecker()
	rule.checkScriptString(&workflowlang.String{Value: "echo '${{ github.event.head_commit.committer.email }}' '${{ github.event.workflow_run.head_branch }}'", Pos: &workflowlang.Pos{Line: 1, Col: 1}}, "jobs.<job_id>.steps.run")
	if len(rule.Findings()) != 2 {
		t.Fatalf("got %+v", rule.Findings())
	}
}

func TestWorkflowRunHeadRepositoryInjection(t *testing.T) {
	for _, expression := range []string{
		"github.event.workflow_run.head_repository.default_branch",
		"github['event']['workflow_run']['head_repository']['default_branch']",
	} {
		t.Run(expression, func(t *testing.T) {
			rule := newTestChecker()
			rule.checkScriptString(&workflowlang.String{Value: "echo '${{ " + expression + " }}'", Pos: &workflowlang.Pos{Line: 1, Col: 1}}, "jobs.<job_id>.steps.run")
			if len(rule.Findings()) != 1 || rule.Findings()[0].Rule != "script-injection" {
				t.Fatalf("got %+v, want one expression injection finding", rule.Findings())
			}
		})
	}
}
