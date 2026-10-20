package fuzz

import (
	"testing"
	"unicode/utf8"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
)

func FuzzExprParse(f *testing.F) {
	f.Add([]byte("github.event.head_commit.message"))
	f.Add([]byte("format('{0} {1}', matrix.os, runner.arch)"))
	f.Add([]byte("success() && !cancelled()"))

	f.Fuzz(func(_ *testing.T, data []byte) {
		if !utf8.Valid(data) {
			return
		}

		l := exprlang.NewLexer(string(data))
		p := exprlang.NewParser()
		e, err := p.Parse(l)
		if err != nil {
			return
		}

		c := exprlang.NewChecker(true, nil)
		c.Check(e)
	})
}
