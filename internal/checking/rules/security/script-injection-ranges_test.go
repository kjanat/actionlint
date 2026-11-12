package security

import (
	"strings"
	"testing"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"

	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

func TestUntrustedExpressionSpans(t *testing.T) {
	t.Parallel()
	for _, expression := range []string{`github.event.issue.title`, `github['event']['issue']['title']`, `github.event.*.body`, `github.event.issue`} {
		for _, serialized := range []bool{false, true} {
			if !serialized && expression == `github.event.issue` {
				continue
			}
			source := expression
			if serialized {
				source = "toJSON(" + expression + ")"
			}
			node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(source + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			findings := checkUntrustedInputs(node)
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

func TestExpressionSpanParserReuse(t *testing.T) {
	t.Parallel()
	p := exprsyntax.NewParser()
	for _, source := range []string{
		"", "(", "foo.", "contains('a',)", "'unclosed", "foo\x00",
		" (((github.event.issue.title))) ", "(startWith('a')).x", "env[format('{0}', (true))]",
		"\n( fromJSON('{\"x\":1}') )['bad']", "!((github.event.issue.title))",
		"foo &&\n", "((1))", "github.event[github.event.issue.title].body",
	} {
		source += "}}"
		node, err := p.Parse(exprsyntax.NewLexer(source))
		fresh, freshErr := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(source))
		if err != nil {
			if freshErr == nil || *err != *freshErr {
				t.Fatalf("reuse changed error for %q: %+v / %+v", source, err, freshErr)
			}
			if err.Offset < 0 || err.EndOffset < err.Offset || err.EndOffset > len(source) {
				t.Fatalf("invalid error span for %q: %+v", source, err)
			}
			continue
		}
		if freshErr != nil || exprsyntax.ExpressionSpan(node) != exprsyntax.ExpressionSpan(fresh) {
			t.Fatalf("reuse changed parse span for %q", source)
		}
		_, findings := exprlang.NewChecker(contextspec.DefaultExpressionScope(), nil).Check(expreval.EvaluateSubexpressions(node))
		findings = append(findings, checkUntrustedInputs(node)...)
		for _, finding := range findings {
			if finding.Offset < 0 || finding.EndOffset < finding.Offset || finding.EndOffset > len(source)-2 {
				t.Fatalf("invalid semantic span for %q: %+v", source, finding)
			}
		}
	}
}
