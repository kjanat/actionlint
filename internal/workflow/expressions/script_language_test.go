package expressions

import (
	"strings"
	"testing"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

func TestShellWordProofsRequireShellSink(t *testing.T) {
	for _, shell := range []string{"bash", "sh", "pwsh", "cmd", "python", "", "bash -c 'python3 {0}'", "bash -ec 'python3 {0}'", "sh -lc 'node {0}'", "bash wrapper.sh {0}"} {
		for _, expression := range []string{"github.repository", "github.repositoryUrl", "github.sha", "runner.os", "hashFiles('**/go.sum')", "'safe'"} {
			t.Run(shell+"/"+expression, func(t *testing.T) {
				rule := newTestChecker()
				key := "jobs.<job_id>.steps.run"
				if shell == "" {
					key = "jobs.<job_id>.steps.with"
				}
				rule.checkScriptString(&workflowlang.String{Value: "const value = ${{ " + expression + " }}", Pos: &workflowlang.Pos{Line: 1, Col: 1}}, key, shell)
				unsafe := shell != "bash" && shell != "sh"
				findings := rule.Findings()
				if (len(findings) != 0) != unsafe || unsafe && (len(findings) != 1 || findings[0].Rule != "script-interpolation") {
					t.Fatalf("shell=%q expression=%q unsafe=%t: %+v", shell, expression, unsafe, findings)
				}
			})
		}
	}
}

func TestGitHubScriptDoesNotUseShellWordProof(t *testing.T) {
	source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/github-script@v8\n        with:\n          script: |\n            const repo = ${{ github.repository }}\n"
	workflow, errs := workflowlang.Parse([]byte(source))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	rule := newTestChecker()
	job := workflow.Jobs["test"]
	for _, err := range []error{rule.VisitWorkflowPre(workflow), rule.VisitJobPre(job), rule.VisitStep(job.Steps[0])} {
		if err != nil {
			t.Fatal(err)
		}
	}
	findings := rule.Findings()
	if len(findings) != 1 || findings[0].Rule != "script-interpolation" || !strings.Contains(findings[0].Message, "script syntax") {
		t.Fatalf("expected JavaScript interpolation finding: %+v", findings)
	}
}
