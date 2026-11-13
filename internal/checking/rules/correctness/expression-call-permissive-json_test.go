package correctness

import (
	"strings"
	"testing"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

func TestPermissiveJSONCalls(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input string
		count int
	}{
		{`{"value":[1,true,null]}`, 0},
		{" \r\n[1]\t", 0},
		{`{"url":"https://example.com/*ordinary string*/"}`, 0},
		{`{/* comment */ "value":1}`, 1},
		{`{'value':1}`, 1},
		{`{value:1}`, 1},
		{`[1,] trailing text`, 1},
		{`[017,0x10,NaN]`, 1},
		{`new Thing(1)`, 1},
		{`{"value":`, 0},
		{`broken`, 0},
	} {
		expression := "fromJSON('" + strings.ReplaceAll(tc.input, "'", "''") + "')"
		node := exprsyntax.ParseAssignedExpression("${{ " + expression + " }}")
		findings := PermissiveJSONCalls(node)
		if len(findings) != tc.count {
			t.Fatalf("%s: got %+v, want %d", expression, findings, tc.count)
		}
		for _, finding := range findings {
			if finding.Rule != "expression-call" || finding.Code != "permissive-json" || finding.Message != "you naughty boy, I see you" {
				t.Fatalf("unexpected message: %+v", finding)
			}
		}
	}
	for _, tc := range []struct {
		expression string
		count      int
	}{
		{`fromJSON(inputs.value)`, 0},
		{`fromJSON(42)`, 0},
		{`fromJSON('[1,]', 'extra')`, 0},
		{`FrOmJsOn(format('[{0},]', 1))`, 1},
		{`fromJSON(toJSON(fromJSON('[1,]')))`, 1},
	} {
		node := exprsyntax.ParseAssignedExpression("${{ " + tc.expression + " }}")
		if findings := PermissiveJSONCalls(node); len(findings) != tc.count {
			t.Fatalf("%s: got %+v, want %d", tc.expression, findings, tc.count)
		}
	}
}
