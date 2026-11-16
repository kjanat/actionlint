package rules_test

import (
	"strconv"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/define"

	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture/declarations"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	"actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func TestWorkflowRunLiteralPatternValidation(t *testing.T) {
	t.Parallel()
	for _, pattern := range []string{"Build [", "?Build", "Build*", "!Build*", "Build's CI", "${{ vars.WORKFLOW }}"} {
		reference := "${{ '" + strings.ReplaceAll(pattern, "'", "''") + "' }}"
		if strings.HasPrefix(pattern, "${{") {
			reference = pattern
		}
		workflow, errs := parser.ParseWorkflow([]byte("on: {workflow_run: {workflows: [" + strconv.Quote(reference) + `]}}
jobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}
`))
		if workflow == nil || len(errs) != 0 {
			t.Fatalf("parse %q: %v", reference, errs)
		}
		rule := correctness.GlobRule.NewVisitor(define.RuleInputs{Rules: checking.ConfigurationRules()})
		if err := rule.VisitWorkflowPre(declarations.Workflow(workflow)); err != nil {
			t.Fatal(err)
		}
		want := pattern == "Build [" || pattern == "?Build" || pattern == "!Build*"
		if (len(rule.Errs()) != 0) != want {
			t.Fatalf("literal pattern %q: %v", pattern, rule.Errs())
		}
		if want {
			pos := workflow.On[0].(*definition.WebhookEvent).Workflows[0].Pos
			if rule.Errs()[0].Start.Line != pos.Line || rule.Errs()[0].Start.Column != pos.Column {
				t.Fatalf("evaluated pattern finding should retain expression position: %v", rule.Errs()[0])
			}
		}
	}
}
