package typecheck

import (
	"strings"
	"testing"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

func TestGroupedCallDiagnosticSpans(t *testing.T) {
	for _, suffix := range []string{"", ".x", "[0]", "['x'].y"} {
		for _, tc := range []struct{ expr, fragment, want string }{
			{"((startWith('a', 'b')))" + suffix, "undefined function", "startWith"},
			{"((format('{0}', 1, 2)))" + suffix, "placeholder", "format"},
			{"((contains('a')))" + suffix, "number of arguments", "contains"},
			{"fromJSON('{\"a\":1}')['b']" + suffix, "property \"b\"", "fromJSON('{\"a\":1}')['b']"},
		} {
			node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(tc.expr + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			_, findings := NewChecker(nil).Check(node)
			found := false
			for _, finding := range findings {
				if strings.Contains(finding.Message, tc.fragment) {
					found = true
					if got := tc.expr[finding.Offset:finding.EndOffset]; got != tc.want {
						t.Fatalf("%s: got %q want %q", tc.expr, got, tc.want)
					}
				}
			}
			if !found {
				t.Fatalf("missing diagnostic for %s", tc.expr)
			}
		}
	}
}
