package rules_test

import (
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"
)

func TestRuleMatrixUnknownOrInvalidStrategyExpression(t *testing.T) {
	for _, expression := range []string{
		"fromJSON(vars.STRATEGY)",
		"fromJSON('{}')",
		"fromJSON('[]')",
		`fromJSON('{"matrix":[]}')`,
		`fromJSON('{"matrix":{"os":"linux"}}')`,
		`fromJSON('{"matrix":{"exclude":"linux"}}')`,
	} {
		t.Run(expression, func(t *testing.T) {
			source := `on: push
jobs:
  test:
    strategy: ${{ ` + expression + ` }}
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`
			_, snapshot := matrixTestWorkflow(t, source)
			rule := correctness.NewRuleMatrix(checking.ConfigurationRules().DiagnosticLevel(nil, "mixed-type-matrix-filters") != "off")
			if err := rule.VisitJobPre(matrixJob(t, snapshot, "test")); err != nil {
				t.Fatal(err)
			}
			if errs := rule.Errs(); len(errs) != 0 {
				t.Fatalf("matrix rule reported on an unknown or invalid shape: %v", errs)
			}
		})
	}
}
