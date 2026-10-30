package mask

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestExpressionsUseExpressionBoundaries(t *testing.T) {
	for _, expression := range []string{"${{ '}}' }}", "${{ 'é' }}", "${{\n github.ref\n}}", "${{ 'it''s }}' }}"} {
		source := "prefix " + expression + " suffix"
		got := Expressions(source)
		if !strings.HasPrefix(got, "prefix ") || !strings.HasSuffix(got, " suffix") || utf8.RuneCountInString(got) != utf8.RuneCountInString(source) || strings.Count(got, "\n") != strings.Count(source, "\n") {
			t.Fatalf("mask %q: %q", source, got)
		}
		middle := strings.TrimSuffix(strings.TrimPrefix(got, "prefix "), " suffix")
		if strings.Trim(middle, "_\n\r") != "" {
			t.Fatalf("expression partially exposed: %q", got)
		}
	}
	if got := Expressions("${{ unfinished"); got != "${{ unfinished" {
		t.Fatalf("invalid expression: %q", got)
	}
}

func TestExpressions(t *testing.T) {
	testCases := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"foo", "foo"},
		{"${{}}", "_____"},
		{"${{ matrix.foo }}", "_________________"},
		{"aaa ${{ matrix.foo }} bbb", "aaa _________________ bbb"},
		{"${{}}${{}}", "__________"},
		{"p${{a}}q${{b}}r", "p______q______r"},
		{"${{", "${{"},
		{"}}", "}}"},
		{"aaa${{foo", "aaa${{foo"},
		{"a${{b}}${{c", "a______${{c"},
		{"a${{b}}c}}d", "a______c}}d"},
		{"a}}b${{c}}d", "a}}b______d"},
		{"before ${{\nfoo\n}} after", "before ___\n___\n__ after"},
		{"before ${{\né\n}} after", "before ___\n_\n__ after"},
		// A closing marker inside a string literal does not end the template.
		{"echo ${{ '}}' }}", "echo ___________"},
		{"echo ${{ format('{0}}}', x) }} $X", "echo " + strings.Repeat("_", 25) + " $X"},
		{"echo ${{ '}}' ==\n  'x' }} $VALUE", "echo ___________\n________ $VALUE"},
		// A template the lexer rejects ends at its first closing marker.
		{"echo ${{ 'open }} rest", "echo ____________ rest"},
		{"echo ${{ a + b }} rest", "echo ____________ rest"},
	}
	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d_%s", i, tc.input), func(t *testing.T) {
			if have := Expressions(tc.input); have != tc.want {
				t.Fatalf("masked result is unexpected.\nwant: %q\nhave: %q", tc.want, have)
			}
		})
	}
}
