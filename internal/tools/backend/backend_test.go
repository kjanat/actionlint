package backend

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMaskExpressionsUsesExpressionBoundaries(t *testing.T) {
	for _, expression := range []string{"${{ '}}' }}", "${{ 'é' }}", "${{\n github.ref\n}}", "${{ 'it''s }}' }}"} {
		source := "prefix " + expression + " suffix"
		got := maskExpressions(source)
		if !strings.HasPrefix(got, "prefix ") || !strings.HasSuffix(got, " suffix") || utf8.RuneCountInString(got) != utf8.RuneCountInString(source) || strings.Count(got, "\n") != strings.Count(source, "\n") {
			t.Fatalf("mask %q: %q", source, got)
		}
		middle := strings.TrimSuffix(strings.TrimPrefix(got, "prefix "), " suffix")
		if strings.Trim(middle, "_\n\r") != "" {
			t.Fatalf("expression partially exposed: %q", got)
		}
	}
	if got := maskExpressions("${{ unfinished"); got != "${{ unfinished" {
		t.Fatalf("invalid expression: %q", got)
	}
}
