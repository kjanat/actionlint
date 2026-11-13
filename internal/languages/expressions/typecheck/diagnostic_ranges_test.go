package typecheck

import (
	"strings"
	"testing"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

func TestSemanticDiagnosticSpans(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ expression, message, want string }{
		{`fromJSON('{"a":1}')['b']`, `property "b"`, `fromJSON('{"a":1}')['b']`},
		{`steps.foo.outputs['bar']`, `property "foo"`, `steps.foo`},
		{`startWith('a', 'b').x`, `undefined function`, `startWith`},
		{`contains('a')[0]`, `number of arguments`, `contains`},
		{`env[fromJSON('true')]`, `property access of object`, `fromJSON('true')`},
		{`(fromJSON('{"a":1}'))['b']`, `property "b"`, `(fromJSON('{"a":1}'))['b']`},
		{`env[(true)]`, `property access of object`, `(true)`},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			t.Parallel()
			node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(tc.expression + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			_, findings := NewChecker(contextspec.DefaultExpressionScope(), nil).Check(expreval.EvaluateSubexpressions(node))
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
		node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(source + "}}"))
		if err != nil {
			return
		}
		exprsyntax.Visit(node, func(node, parent exprsyntax.Node, entering bool) {
			if !entering {
				return
			}
			span := exprsyntax.ExpressionSpan(node)
			if span.Start < 0 || span.End < span.Start || span.End > len(source) {
				t.Fatalf("invalid %T span %+v", node, span)
			}
			if parent != nil {
				outer := exprsyntax.ExpressionSpan(parent)
				if span.Start < outer.Start || span.End > outer.End {
					t.Fatalf("%T %+v outside %T %+v", node, span, parent, outer)
				}
			}
			parsed, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(source[span.Start:span.End] + "}}"))
			if err != nil || parsed == nil {
				t.Fatalf("%T span %q cannot be parsed: %v", node, source[span.Start:span.End], err)
			}
		})
	})
}

func TestExpressionSyntaxErrorSpans(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ source, want string }{
		{`foo + bar}}`, `+`},
		{`foo § bar}}`, `§`},
		{`foo.}}`, `}}`},
		{`contains('a', )}}`, `)`},
		{`'unclosed`, ``},
	} {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			_, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(tc.source))
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
	t.Parallel()
	for _, source := range []string{"github.actor &&", "github.actor &&\n", "'unclosed", "'unclosed\n", "contains('é"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			_, err, _ := exprsyntax.ParseConditionExpression(source)
			if err == nil {
				t.Fatal("expected syntax error")
			}
			if err.Offset != len(source) || err.EndOffset != len(source) {
				t.Fatalf("EOF span %d:%d, want %d:%d", err.Offset, err.EndOffset, len(source), len(source))
			}
		})
	}
}
