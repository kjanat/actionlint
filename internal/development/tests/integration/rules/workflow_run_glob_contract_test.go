package rules_test

import (
	"strconv"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/define"

	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"
	"actionlint.kjanat.dev/v2/internal/input/sourcetext"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture/declarations"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	"actionlint.kjanat.dev/v2/internal/model/workflows/parser"
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
			workflow, errs := parser.ParseWorkflow([]byte(source))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			rule := correctness.GlobRule.NewVisitor(define.RuleInputs{Rules: checking.ConfigurationRules()})
			if err := rule.VisitWorkflowPre(declarations.Workflow(workflow)); err != nil {
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
	rule := correctness.GlobRule.NewVisitor(define.RuleInputs{Rules: checking.ConfigurationRules()})
	workflow := &definition.Workflow{On: []definition.Event{&definition.WebhookEvent{Workflows: []*definition.String{{Value: "${{ inputs.pattern }}", Pos: &sourcetext.Position{Line: 1, Column: 1}}}}}}
	if err := rule.VisitWorkflowPre(declarations.Workflow(workflow)); err != nil || len(rule.Errs()) != 0 {
		t.Fatalf("unknown expression treated as a literal glob: %v, %v", rule.Errs(), err)
	}
}
