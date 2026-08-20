package actionlint_fuzz

import (
	"testing"
	"unicode/utf8"

	"actionlint.kjanat.dev"
)

func FuzzExprParse(f *testing.F) {
	f.Add([]byte("github.event.head_commit.message"))
	f.Add([]byte("format('{0} {1}', matrix.os, runner.arch)"))
	f.Add([]byte("success() && !cancelled()"))

	f.Fuzz(func(_ *testing.T, data []byte) {
		if !utf8.Valid(data) {
			return
		}

		l := actionlint.NewExprLexer(string(data))
		p := actionlint.NewExprParser()
		e, err := p.Parse(l)
		if err != nil {
			return
		}

		c := actionlint.NewExprSemanticsChecker(true, nil)
		c.Check(e)
	})
}
