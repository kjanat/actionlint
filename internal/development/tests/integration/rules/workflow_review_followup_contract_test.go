package rules_test

import (
	"strconv"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	fixturedecl "actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture/declarations"

	declarationchecks "actionlint.kjanat.dev/v2/internal/checking/executor/declarations"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func TestWorkflowRunLiteralPatternValidation(t *testing.T) {
	t.Parallel()
	for _, pattern := range []string{"Build [", "?Build", "Build*", "!Build*", "Build's CI", "${{ vars.WORKFLOW }}"} {
		reference := "${{ '" + strings.ReplaceAll(pattern, "'", "''") + "' }}"
		if strings.HasPrefix(pattern, "${{") {
			reference = pattern
		}
		workflow, errs := workflowparser.Parse([]byte("on: {workflow_run: {workflows: [" + strconv.Quote(reference) + `]}}
jobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}
`))
		if workflow == nil || len(errs) != 0 {
			t.Fatalf("parse %q: %v", reference, errs)
		}
		rule := declarationchecks.New(checking.ConfigurationRules(), correctness.GlobCheck())
		if err := rule.VisitWorkflowPre(fixturedecl.Workflow(workflow)); err != nil {
			t.Fatal(err)
		}
		want := pattern == "Build [" || pattern == "?Build" || pattern == "!Build*"
		if (len(rule.Errs()) != 0) != want {
			t.Fatalf("literal pattern %q: %v", pattern, rule.Errs())
		}
		if want {
			pos := workflow.On[0].(*workflowlang.WebhookEvent).Workflows[0].Pos
			if rule.Errs()[0].Start.Line != pos.Line || rule.Errs()[0].Start.Column != pos.Col {
				t.Fatalf("evaluated pattern finding should retain expression position: %v", rule.Errs()[0])
			}
		}
	}
}
