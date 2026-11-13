package expressions

import (
	"testing"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"

	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestCompositeActionInputPreservesPreviousFindings(t *testing.T) {
	t.Parallel()
	workflow, errors := workflowparser.Parse([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    env: {JOB: safe}\n    strategy:\n      matrix: {os: [ubuntu-latest]}\n    steps:\n      - uses: actions/github-script@v8\n        env: {STEP: safe}\n        with:\n          script: ${{ github.event.issue.title }}\n"))
	if len(errors) != 0 {
		t.Fatal(errors)
	}
	rule := newTestChecker()
	view := ownedWorkflowFixture(workflow)
	job := testJob(t, view, "test")
	rule.BeginComposite(workflowdecl.ActionBodyView{})
	rule.Error(&workflowlang.Pos{Line: 1, Col: 1}, "existing finding")
	originalFinding := rule.errs[0]
	if err := rule.CheckCompositeStep(testStep(t, job, 0)); err != nil {
		t.Fatal(err)
	}
	if len(rule.errs) != 2 || rule.errs[0].message != originalFinding.message || rule.errs[1].rule != "script-injection" {
		t.Fatalf("script finding lost: %+v", rule.errs)
	}
	if rule.scriptEnv.Present() {
		t.Fatal("composite action input checking kept the previous step's environment")
	}
}
