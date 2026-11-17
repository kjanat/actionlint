package suspicious

import (
	"strings"
	"testing"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
)

func TestSuspiciousCoercion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input    string
		warnings int
	}{
		{"contains(null, '')", 1},
		{"startsWith(true, 'TR')", 1},
		{"endsWith('true', true)", 1},
		{"hashFiles('foo', null)", 1},
		{"fromJSON('[]') == 0", 1},
		{"fromJSON('{}') >= 0", 1},
		{"true > false", 1},
		{"null <= 0", 1},
		{"contains('hello', 'ell')", 0},
		{"contains(fromJSON('[true]'), true)", 0},
		{"endsWith(711, '11')", 0},
		{"fromJSON('{}') == fromJSON('{}')", 0},
		{"fromJSON('[]') == fromJSON('[1]')", 0},
		{"fromJSON('{}') == null", 0},
		{"null != fromJSON('[]')", 0},
		{"fromJSON('[]') < null", 1},
		{"github.event.unknown >= 0", 0},
		{"startsWith(github.event.unknown, 'x')", 0},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(tc.input + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			checker := exprlang.NewChecker(contextspec.DefaultExpressionScope(), nil)
			checker.SetAvailability(contextspec.WorkflowExpressionScope("jobs.<job_id>.steps.if"))
			checked := checker.CheckResult(expreval.EvaluateSubexpressions(node))
			if !checked.Valid || len(checked.Findings) != 0 {
				t.Fatalf("type checking emitted policy findings: %+v", checked)
			}
			findings := SuspiciousCoercion(node, checked.Types, checked.Calls)
			if len(findings) != tc.warnings {
				t.Fatalf("findings=%v, want %d", findings, tc.warnings)
			}
			for _, finding := range findings {
				if finding.Rule != "coercion" || finding.Code != "coercion" || finding.EndOffset <= finding.Offset {
					t.Fatalf("invalid warning: %+v", finding)
				}
			}
		})
	}
}

func TestCoercionFollowsArgumentDemand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		source      string
		warnings    int
		conditional bool
	}{
		{"false && startsWith(true, 'TR')", 0, false},
		{"true || startsWith(true, 'TR')", 0, false},
		{"github.event.unknown && startsWith(true, 'TR')", 1, true},
		{"case(false, startsWith(true, 'TR'), false)", 0, false},
		{"case(github.event.unknown, startsWith(true, 'TR'), false)", 1, true},
		{"startsWith(fromJSON('{}'), startsWith(true, 'TR'))", 0, false},
		{"format('{0}', 'safe', startsWith(true, 'TR'))", 0, false},
		{"endsWith(startsWith(true, 'TR'), true)", 3, false},
	} {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			node := exprsyntax.ParseAssignedExpression("${{ " + tc.source + " }}")
			checker := exprlang.NewChecker(contextspec.DefaultExpressionScope(), nil)
			checker.SetAvailability(contextspec.WorkflowExpressionScope("jobs.<job_id>.steps.if"))
			checked := checker.CheckResult(expreval.EvaluateSubexpressions(node))
			if !checked.Valid {
				t.Fatal(checked.Findings)
			}
			findings := SuspiciousCoercion(node, checked.Types, checked.Calls)
			if len(findings) != tc.warnings {
				t.Fatalf("findings=%v, want %d", findings, tc.warnings)
			}
			for _, finding := range findings {
				if got := strings.Contains(finding.Message, "may convert"); got != tc.conditional {
					t.Fatalf("conditional=%t, finding=%s", tc.conditional, finding.Message)
				}
			}
		})
	}
}
