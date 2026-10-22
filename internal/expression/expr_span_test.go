package expression

import (
	"strings"
	"testing"
)

func TestSemanticDiagnosticSpans(t *testing.T) {
	for _, tc := range []struct{ expression, message, want string }{
		{`fromJSON('{"a":1}')['b']`, `property "b"`, `fromJSON('{"a":1}')['b']`},
		{`steps.foo.outputs['bar']`, `property "foo"`, `steps.foo`},
		{`format('{0}{1}', 1, 2, 3)[0]`, `does not contain placeholder`, `format`},
		{`startWith('a', 'b').x`, `undefined function`, `startWith`},
		{`contains('a')[0]`, `number of arguments`, `contains`},
		{`env[fromJSON('true')]`, `property access of object`, `fromJSON('true')`},
		{`startsWith(fromJSON('true'), 'x')`, `argument of function call`, `fromJSON('true')`},
		{`(fromJSON('{"a":1}'))['b']`, `property "b"`, `(fromJSON('{"a":1}'))['b']`},
		{`env[(true)]`, `property access of object`, `(true)`},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			node, err := NewParser().Parse(NewLexer(tc.expression + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			_, findings := NewChecker(false, nil).Check(node)
			for _, finding := range findings {
				if !strings.Contains(finding.Message, tc.message) {
					continue
				}
				if finding.Offset < 0 || finding.EndOffset < finding.Offset || finding.EndOffset > len(tc.expression) {
					t.Fatalf("invalid span: %+v", finding)
				}
				if got := tc.expression[finding.Offset:finding.EndOffset]; got != tc.want {
					t.Fatalf("span %q; want %q: %s", got, tc.want, finding.Message)
				}
				return
			}
			t.Fatalf("missing %q: %v", tc.message, findings)
		})
	}
}

func FuzzExpressionParserSpans(f *testing.F) {
	for _, source := range []string{`foo.bar[0]`, `format('x', 1)`, `(foo).bar`, `!foo && env['x']`, `fromJSON('{"x":1}').x`, "foo\n .bar", `env['é']`} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 4096 {
			t.Skip()
		}
		node, err := NewParser().Parse(NewLexer(source + "}}"))
		if err != nil {
			return
		}
		Visit(node, func(node, parent Node, entering bool) {
			if !entering {
				return
			}
			span := ExpressionSpan(node)
			if span.Start < 0 || span.End < span.Start || span.End > len(source) {
				t.Fatalf("invalid %T span %+v", node, span)
			}
			if parent != nil {
				outer := ExpressionSpan(parent)
				if span.Start < outer.Start || span.End > outer.End {
					t.Fatalf("%T %+v outside %T %+v", node, span, parent, outer)
				}
			}
			parsed, err := NewParser().Parse(NewLexer(source[span.Start:span.End] + "}}"))
			if err != nil || parsed == nil {
				t.Fatalf("%T span %q cannot be parsed: %v", node, source[span.Start:span.End], err)
			}
		})
	})
}

func TestExpressionSyntaxErrorSpans(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{`foo + bar}}`, `+`},
		{`foo § bar}}`, `§`},
		{`foo.}}`, `}}`},
		{`contains('a', )}}`, `)`},
		{`'unclosed`, ``},
	} {
		t.Run(tc.source, func(t *testing.T) {
			_, err := NewParser().Parse(NewLexer(tc.source))
			if err == nil {
				t.Fatal("expected syntax error")
			}
			if err.Offset < 0 || err.EndOffset < err.Offset || err.EndOffset > len(tc.source) {
				t.Fatalf("invalid span: %+v", err)
			}
			if got := tc.source[err.Offset:err.EndOffset]; got != tc.want {
				t.Fatalf("range %q, want %q (%s)", got, tc.want, err.Message)
			}
		})
	}
}

func TestBareConditionSyntheticDelimiterSpans(t *testing.T) {
	for _, source := range []string{"github.actor &&", "github.actor &&\n", "'unclosed", "'unclosed\n", "contains('é"} {
		t.Run(source, func(t *testing.T) {
			_, err, _ := ParseConditionExpression(source)
			if err == nil {
				t.Fatal("expected syntax error")
			}
			if err.Offset != len(source) || err.EndOffset != len(source) {
				t.Fatalf("EOF span %d:%d, want %d:%d", err.Offset, err.EndOffset, len(source), len(source))
			}
		})
	}
}

func TestUntrustedExpressionSpans(t *testing.T) {
	for _, expression := range []string{`github.event.issue.title`, `github['event']['issue']['title']`, `github.event.*.body`, `github.event.issue`} {
		for _, serialized := range []bool{false, true} {
			if !serialized && expression == `github.event.issue` {
				continue
			}
			source := expression
			if serialized {
				source = "toJSON(" + expression + ")"
			}
			node, err := NewParser().Parse(NewLexer(source + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			_, findings := NewChecker(true, nil).Check(node)
			found := false
			for _, finding := range findings {
				if !strings.Contains(finding.Message, "potentially untrusted") {
					continue
				}
				found = true
				if got := source[finding.Offset:finding.EndOffset]; got != expression {
					t.Fatalf("%s range %q, want %q", source, got, expression)
				}
			}
			if !found {
				t.Fatalf("no untrusted finding for %s", source)
			}
		}
	}
}
