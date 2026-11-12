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

func TestWorkflowRunGlobRequiresPositivePattern(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		patterns []string
		invalid  bool
	}{
		{[]string{"!Build"}, true},
		{[]string{"!Build", "!Test*"}, true},
		{[]string{"${{ '!Build' }}"}, true},
		{[]string{"Build", "!Test*"}, false},
		{[]string{"Build*", "!Build docs"}, false},
		{[]string{`\!Build`}, false},
		{[]string{"!Build", "${{ inputs.workflow }}"}, false},
	} {
		t.Run(strings.Join(tc.patterns, ","), func(t *testing.T) {
			t.Parallel()
			var patterns []string
			for _, pattern := range tc.patterns {
				patterns = append(patterns, strconv.Quote(pattern))
			}
			source := `on:
  workflow_run:
    workflows: [` + strings.Join(patterns, ", ") + `]
    types: [completed]
jobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}
`
			workflow, errs := workflowparser.Parse([]byte(source))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			rule := declarationchecks.New(checking.ConfigurationRules(), correctness.GlobCheck())
			if err := rule.VisitWorkflowPre(fixturedecl.Workflow(workflow)); err != nil {
				t.Fatal(err)
			}
			if got := rule.Errs(); (len(got) != 0) != tc.invalid || len(got) > 1 || len(got) == 1 && (got[0].Start.Line != 3 || !strings.Contains(got[0].Message, "positive")) {
				t.Fatalf("positive-pattern validation: %+v", got)
			}
		})
	}
}

func TestWorkflowRunGlobUnknownExpression(t *testing.T) {
	t.Parallel()
	rule := declarationchecks.New(checking.ConfigurationRules(), correctness.GlobCheck())
	workflow := &workflowlang.Workflow{On: []workflowlang.Event{&workflowlang.WebhookEvent{Workflows: []*workflowlang.String{{Value: "${{ inputs.pattern }}", Pos: &workflowlang.Pos{Line: 1, Col: 1}}}}}}
	if err := rule.VisitWorkflowPre(fixturedecl.Workflow(workflow)); err != nil || len(rule.Errs()) != 0 {
		t.Fatalf("unknown expression treated as a literal glob: %v, %v", rule.Errs(), err)
	}
}
