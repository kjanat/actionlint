package output

import (
	"strings"
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestSuppliedSourceDeterminesSnippet(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		source []byte
		line   int
		want   string
	}{
		{"current line", []byte("first\nsecond\n"), 2, "second\n^"},
		{"outside source", []byte("first\n"), 20, ""},
		{"captured fallback", nil, 2, "captured\n^"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := FindingOf(results.Diagnostic{Start: sourcepos.Position{Line: tc.line, Column: 1}, End: sourcepos.Position{Line: tc.line, Column: 2}, Snippet: "captured"}, tc.source)
			if got := f.TemplateFields().Snippet; got != tc.want {
				t.Fatalf("snippet=%q, want %q", got, tc.want)
			}
			var out strings.Builder
			f.PrettyPrint(&out)
			if tc.source != nil && strings.Contains(out.String(), "captured") {
				t.Fatalf("rendered stale snippet: %q", out.String())
			}
		})
	}
}
