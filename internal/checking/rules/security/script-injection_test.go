package security

import (
	"slices"
	"testing"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
)

func TestScriptInjectionCombinesIndependentValueSources(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		source         string
		semanticErrors int
		codes          []string
	}{
		{"format('{0}{1}', github.event.issue.title, secrets.TOKEN)", 0, []string{"secret-interpolation", "untrusted-interpolation"}},
		{"format('{0}{1}', github.event.issue.title, secrets.TOKEN.invalid)", 1, []string{"untrusted-interpolation"}},
		{"format('{0}{1}', github.events.title, secrets.TOKEN)", 1, []string{"secret-interpolation"}},
	} {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			node := exprsyntax.ParseAssignedExpression("${{ " + tc.source + " }}")
			checker := exprlang.NewChecker(contextspec.DefaultExpressionScope(), nil)
			checker.SetAvailability(contextspec.WorkflowExpressionScope("jobs.<job_id>.steps.run"))
			result := checker.CheckResult(expreval.EvaluateSubexpressions(node))
			if len(result.Findings) != tc.semanticErrors {
				t.Fatalf("unexpected semantic findings: %v", result.Findings)
			}
			var codes []string
			for _, finding := range ScriptInjection(node, result.Findings) {
				if finding.Rule != "script-injection" {
					t.Fatalf("wrong rule: %+v", finding)
				}
				codes = append(codes, finding.Code)
			}
			slices.Sort(codes)
			if !slices.Equal(codes, tc.codes) {
				t.Fatalf("codes=%v, want %v", codes, tc.codes)
			}
		})
	}
}
