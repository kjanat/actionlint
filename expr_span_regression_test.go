package actionlint

import (
	"strings"
	"testing"
)

func TestExpressionSpanParserReuse(t *testing.T) {
	p := NewExprParser()
	for _, source := range []string{
		"", "(", "foo.", "contains('a',)", "'unclosed", "foo\x00",
		" (((github.event.issue.title))) ", "(startWith('a')).x", "env[format('{0}', (true))]",
		"\n( fromJSON('{\"x\":1}') )['bad']", "!((github.event.issue.title))",
		"foo &&\n", "((1))", "github.event[github.event.issue.title].body",
	} {
		source += "}}"
		node, err := p.Parse(NewExprLexer(source))
		fresh, freshErr := NewExprParser().Parse(NewExprLexer(source))
		if err != nil {
			if freshErr == nil || *err != *freshErr {
				t.Fatalf("reuse changed error for %q: %+v / %+v", source, err, freshErr)
			}
			if err.Offset < 0 || err.EndOffset < err.Offset || err.EndOffset > len(source) {
				t.Fatalf("invalid error span for %q: %+v", source, err)
			}
			continue
		}
		if freshErr != nil || expressionSpan(node) != expressionSpan(fresh) {
			t.Fatalf("reuse changed parse span for %q", source)
		}
		_, findings := NewExprSemanticsChecker(true, nil).Check(node)
		for _, finding := range findings {
			if finding.Offset < 0 || finding.EndOffset < finding.Offset || finding.EndOffset > len(source)-2 {
				t.Fatalf("invalid semantic span for %q: %+v", source, finding)
			}
		}
	}
}

func TestGroupedCallDiagnosticSpans(t *testing.T) {
	for _, suffix := range []string{"", ".x", "[0]", "['x'].y"} {
		for _, tc := range []struct{ expr, fragment, want string }{
			{"((startWith('a', 'b')))" + suffix, "undefined function", "startWith"},
			{"((format('{0}', 1, 2)))" + suffix, "placeholder", "format"},
			{"((contains('a')))" + suffix, "number of arguments", "contains"},
			{"fromJSON('{\"a\":1}')['b']" + suffix, "property \"b\"", "fromJSON('{\"a\":1}')['b']"},
		} {
			node, err := NewExprParser().Parse(NewExprLexer(tc.expr + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			_, findings := NewExprSemanticsChecker(false, nil).Check(node)
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
