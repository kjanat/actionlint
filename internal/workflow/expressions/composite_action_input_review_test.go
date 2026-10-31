package expressions

import (
	"reflect"
	"testing"

	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestCompositeActionInputRestoresCheckerContext(t *testing.T) {
	workflow, errors := workflowparser.Parse([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    env: {JOB: safe}\n    strategy:\n      matrix: {os: [ubuntu-latest]}\n    steps:\n      - uses: actions/github-script@v8\n        env: {STEP: safe}\n        with:\n          script: ${{ github.event.issue.title }}\n"))
	if len(errors) != 0 {
		t.Fatal(errors)
	}
	rule := newTestChecker()
	view := ownedWorkflowFixture(workflow)
	job := testJob(t, view, "test")
	if err := rule.VisitWorkflowPre(view); err != nil {
		t.Fatal(err)
	}
	if err := rule.VisitJobPre(job); err != nil {
		t.Fatal(err)
	}
	rule.inputsTy = new(exprlang.NewMapObjectType(exprlang.BoolType()))
	rule.scriptEnv = testStep(t, job, 0).Env()
	rule.Error(&workflowlang.Pos{Line: 1, Col: 1}, "existing finding")
	originalFinding := rule.errs[0]
	previous := *rule
	rule.CheckCompositeScript(testStep(t, job, 0))
	if len(rule.errs) != 2 || rule.errs[0] != originalFinding || rule.errs[1].rule != "script-injection" {
		t.Fatalf("script finding lost: %+v", rule.errs)
	}
	current := *rule
	previous.errs, current.errs = nil, nil
	if !reflect.DeepEqual(current, previous) {
		t.Fatal("composite action input checking changed caller context")
	}
}
