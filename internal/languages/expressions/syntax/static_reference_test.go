package syntax

import (
	"slices"
	"testing"
)

func TestReferencePath(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		expression string
		want       []string
	}{
		{`github.event`, []string{"github", "event"}},
		{`GITHUB['Event']`, []string{"github", "event"}},
		{`matrix['a.b']`, []string{"matrix", "a.b"}},
		{`matrix.a.b`, []string{"matrix", "a", "b"}},
		{`matrix['']`, []string{"matrix", ""}},
		{`matrix[inputs.key]`, nil},
		{`matrix.*.name`, nil},
		{`fromJSON('{}')`, nil},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			t.Parallel()
			node, err := NewParser().Parse(NewLexer(tc.expression + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			got, ok := ReferencePath(node)
			if ok != (tc.want != nil) || !slices.Equal(got, tc.want) {
				t.Fatalf("ReferencePath = %q, %t; want %q", got, ok, tc.want)
			}
		})
	}
}
