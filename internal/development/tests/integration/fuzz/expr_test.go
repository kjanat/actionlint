package fuzz

import (
	"testing"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	"actionlint.kjanat.dev/v2/internal/model/contexts"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
)

func FuzzExprParse(f *testing.F) {
	f.Add([]byte("github.event.head_commit.message"))
	f.Add([]byte("format('{0} {1}', matrix.os, runner.arch)"))
	f.Add([]byte("success() && !cancelled()"))

	f.Fuzz(func(_ *testing.T, data []byte) {
		if !utf8.Valid(data) {
			return
		}

		l := syntax.NewLexer(string(data))
		p := syntax.NewParser()
		e, err := p.ParseExpression(l)
		if err != nil {
			return
		}

		c := typecheck.NewChecker(contexts.DefaultExpressionScope(), nil)
		c.Check(evaluate.EvaluateSubexpressions(e))
	})
}
