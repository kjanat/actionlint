package actionlint

import "testing"

func TestAllScriptInterpolationsChecked(t *testing.T) {
	rule := NewRuleExpression(nil, nil)
	rule.checkScriptString(&String{Value: "echo '${{ github.event.head_commit.committer.email }}' '${{ github.event.workflow_run.head_branch }}'", Pos: &Pos{Line: 1, Col: 1}}, "jobs.<job_id>.steps.run")
	if len(rule.Errs()) != 2 {
		t.Fatalf("got %+v", rule.Errs())
	}
}
