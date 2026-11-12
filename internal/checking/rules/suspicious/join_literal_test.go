package suspicious

import (
	"testing"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"
)

func TestJoinLiteral(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		source string
		want   int
	}{
		{"join('[github.server_url,github.repository]', '/')", 1},
		{"join('ordinary text', '/')", 1},
		{"join('')", 1},
		{"JOIN('x', fromJSON('broken'))", 1},
		{"false && join('unused', '/')", 0},
		{"join(env.VALUE, '/')", 0},
		{`join(fromJSON('["a", "b"]'), '/')`, 0},
		{"join(true, '/')", 0},
		{"join('a', '/', 'extra')", 0},
	} {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(tc.source + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			checked := exprlang.NewChecker(contextspec.WorkflowExpressionScope("jobs.<job_id>.steps.env"), nil).CheckResult(expreval.EvaluateSubexpressions(node))
			findings := JoinLiteral(node, checked.Calls)
			if len(findings) != tc.want {
				t.Fatalf("findings=%v, want %d", findings, tc.want)
			}
			if tc.want != 0 {
				finding := findings[0]
				if finding.Rule != "join-literal" || finding.Code != "join-literal" || finding.EndOffset <= finding.Offset || tc.source[finding.Offset] != '\'' {
					t.Fatalf("unexpected range: %+v", finding)
				}
			}
		})
	}
}
