package builtin_test

import (
	"strings"
	"testing"

	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"

	"actionlint.kjanat.dev/v2/internal/builtin"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestWorkflowPermissionsDeclarationPosition(t *testing.T) {
	policy, err := repoconfig.RequirePermissions("workflow")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		prefix string
		line   int
	}{
		{"", 1},
		{"# license\n\n", 3},
		{"# license\n---\n", 3},
	} {
		for _, declaration := range []string{"on: push", `name:
  !!str
  |
    CI
on: push`, `name: &title CI
run-name: *title
on: push`} {
			for _, ending := range []string{"\n", "\r\n"} {
				source := strings.ReplaceAll(tc.prefix+declaration+`
jobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}
`, "\n", ending)
				workflow, findings := workflowparser.ParseDocument([]byte(source))
				if len(findings) != 0 || workflow == nil || workflow.Pos == nil || *workflow.Pos != (workflowlang.Pos{Line: tc.line, Col: 1}) {
					t.Fatalf("missing workflow declaration position: %+v, %+v", workflow, findings)
				}
				rule := builtin.NewRuleRequirePermissions(policy)
				if err := rule.VisitWorkflowPre(ownedWorkflowFixture(workflow)); err != nil {
					t.Fatal(err)
				}
				if findings := rule.Errs(); len(findings) != 1 || findings[0].Start.Line != tc.line || findings[0].Start.Column != 1 {
					t.Fatalf("permission finding lost declaration position: %+v", findings)
				}
			}
		}
	}
	rule := builtin.NewRuleRequirePermissions(policy)
	if err := rule.VisitWorkflowPre(ownedWorkflowFixture(&workflowlang.Workflow{})); err != nil {
		t.Fatal(err)
	}
	if findings := rule.Errs(); len(findings) != 1 || findings[0].Start.Line != 1 || findings[0].Start.Column != 1 {
		t.Fatalf("manual AST fallback changed: %+v", findings)
	}
}
