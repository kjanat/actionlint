package rules_test

import (
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	fixturedecl "actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture/declarations"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"

	declarationchecks "actionlint.kjanat.dev/v2/internal/checking/executor/declarations"

	"actionlint.kjanat.dev/v2/internal/checking/rules/security"
	"actionlint.kjanat.dev/v2/internal/input/configuration"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func TestWorkflowPermissionsDeclarationPosition(t *testing.T) {
	t.Parallel()
	locations := []configuration.PermissionLocation{configuration.WorkflowPermissions}
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
				workflow, findings := workflowparser.Parse([]byte(source))
				if len(findings) != 0 || workflow == nil || workflow.Pos == nil || *workflow.Pos != (sourcepos.Position{Line: tc.line, Column: 1}) {
					t.Fatalf("missing workflow declaration position: %+v, %+v", workflow, findings)
				}
				rule := declarationchecks.New(checking.ConfigurationRules(), security.MissingPermissionsCheck(locations))
				if err := rule.VisitWorkflowPre(fixturedecl.Workflow(workflow)); err != nil {
					t.Fatal(err)
				}
				if findings := rule.Errs(); len(findings) != 1 || findings[0].Start.Line != tc.line || findings[0].Start.Column != 1 {
					t.Fatalf("permission finding lost declaration position: %+v", findings)
				}
			}
		}
	}
	rule := declarationchecks.New(checking.ConfigurationRules(), security.MissingPermissionsCheck(locations))
	if err := rule.VisitWorkflowPre(fixturedecl.Workflow(&workflowlang.Workflow{})); err != nil {
		t.Fatal(err)
	}
	if findings := rule.Errs(); len(findings) != 1 || findings[0].Start.Line != 1 || findings[0].Start.Column != 1 {
		t.Fatalf("manual AST fallback changed: %+v", findings)
	}
}
