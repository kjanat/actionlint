package suspicious

import (
	"testing"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
)

func TestCaseInsensitiveConditionsUseCheckedCalls(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		source           string
		findings, errors int
	}{
		{"github.ref == 'main'", 1, 0},
		{"'main' != github.ref", 1, 0},
		{"github.ref > 'main'", 0, 0},
		{"startsWith(github.ref, 'main')", 1, 0},
		{"contains(github.actor, 'admin')", 1, 0},
		{"endsWith(github.head_ref, 'release')", 1, 0},
		{"contains(fromJSON('[\"main\"]'), github.ref)", 0, 0},
		{"contains(github.ref, 'main', 'extra')", 0, 2},
		{"unknown(github.ref, 'main')", 0, 1},
		{"true || startsWith(github.ref, 'main')", 1, 0},
		{"case(false, startsWith(github.ref, 'main'), false)", 1, 0},
	} {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			root, err, _ := exprsyntax.ParseConditionExpression(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			checker := exprlang.NewChecker(contextspec.DefaultExpressionScope(), nil)
			checker.SetAvailability(contextspec.WorkflowExpressionScope("jobs.<job_id>.steps.if"))
			result := checker.CheckResult(expreval.EvaluateSubexpressions(root))
			if len(result.Findings) != tc.errors {
				t.Fatalf("type findings=%v, want %d", result.Findings, tc.errors)
			}
			findings := CaseInsensitiveConditions(root, result.Types, result.Calls)
			if len(findings) != tc.findings {
				t.Fatalf("findings=%v, want %d", findings, tc.findings)
			}
			for _, finding := range findings {
				if finding.Rule != "case-insensitive-conditions" || !finding.Advisory || finding.EndOffset <= finding.Offset {
					t.Fatalf("invalid finding: %+v", finding)
				}
			}
			if findings := CaseInsensitiveConditions(root, exprlang.InferredTypes{}, exprlang.CheckedCalls{}); len(findings) != 0 {
				t.Fatalf("unchecked syntax acquired findings: %v", findings)
			}
		})
	}
}
