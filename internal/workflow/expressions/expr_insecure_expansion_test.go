package expressions

import (
	"strings"
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestSecretScriptInterpolation(t *testing.T) {
	for _, script := range []string{
		"release-please release-pr --token=${{ secrets.RELEASE_PLEASE_GH_TOKEN }}",
		`cargo workspaces publish --token "${{ secrets.CRATES_IO_TOKEN }}" --yes`,
		`echo '${{ secrets.TOKEN }}'`,
		`echo "${{ github.token }}"`,
	} {
		t.Run(script, func(t *testing.T) {
			rule := newTestChecker()
			rule.checkScriptString(&workflowlang.String{Value: script, Pos: &workflowlang.Pos{Line: 1, Col: 1}}, "jobs.<job_id>.steps.run")
			findings := rule.Findings()
			if len(findings) != 1 || findings[0].Rule != "script-injection" || findings[0].Code != "secret-interpolation" || !strings.Contains(findings[0].Message, "environment variable") {
				t.Fatalf("got %+v, want secret interpolation guidance", findings)
			}
		})
	}
}

func TestSecretEnvironmentValueIsNotScriptInterpolation(t *testing.T) {
	rule := newTestChecker()
	rule.checkString(&workflowlang.String{Value: "${{ secrets.TOKEN }}", Pos: &workflowlang.Pos{Line: 1, Col: 1}}, "jobs.<job_id>.steps.env")
	rule.checkScriptString(&workflowlang.String{Value: `tool --token "$TOKEN"`, Pos: &workflowlang.Pos{Line: 2, Col: 1}}, "jobs.<job_id>.steps.run")
	if findings := rule.Findings(); len(findings) != 0 {
		t.Fatalf("environment-variable handoff reported: %+v", findings)
	}
}

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
