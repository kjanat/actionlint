package expression_test

import (
	"testing"

	exprlang "actionlint.kjanat.dev/internal/expression"
)

func TestParserOwnedExpressionSpans(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{`fromJSON('{"win":1}')['mac']`, `fromJSON('{"win":1}')['mac']`},
		{`steps.foo.outputs['bar']`, `steps.foo.outputs['bar']`},
		{`matrix.x.y`, `matrix.x.y`},
		{`env[inputs.name] && true`, `env[inputs.name] && true`},
		{`fromJSON(format('{0}', '[]'))[0].name`, `fromJSON(format('{0}', '[]'))[0].name`},
		{`env[format('a]b', inputs.name)]`, `env[format('a]b', inputs.name)]`},
		{`startWith('hello')`, `startWith('hello')`},
		{`format('x', 1) && false`, `format('x', 1) && false`},
		{`(fromJSON('{}'))['mac']`, `(fromJSON('{}'))['mac']`},
		{`env . NAME [ 'x' ]`, `env . NAME [ 'x' ]`},
	} {
		t.Run(tc.text, func(t *testing.T) {
			node, err := exprlang.NewParser().Parse(exprlang.NewLexer(tc.text + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			span := exprlang.ExpressionSpan(node)
			if got := tc.text[span.Start:span.End]; got != tc.want {
				t.Fatalf("range %q, want %q", got, tc.want)
			}
		})
	}
}
