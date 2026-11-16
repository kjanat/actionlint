package finding

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestCanonicalSnippetLineEndings(t *testing.T) {
	t.Parallel()
	for _, ending := range []struct {
		name string
		text string
	}{
		{name: "CR", text: "\r"},
		{name: "CRLF", text: "\r\n"},
		{name: "NEL", text: "\u0085"},
		{name: "LS", text: "\u2028"},
		{name: "PS", text: "\u2029"},
	} {
		t.Run(ending.name, func(t *testing.T) {
			t.Parallel()
			source := []byte("\ufefftoken" + ending.text + "tail")
			for _, raw := range []bool{false, true} {
				finding := &Finding{Start: sourcetext.Position{Line: 1}, Source: results.DiagnosticSource{RawScript: raw}}
				got := finding.Publish(source).Snippet
				want := "token"
				if raw {
					want = "\ufefftoken" + ending.text + "tail"
					if ending.text == "\r\n" {
						want = "\ufefftoken"
					}
				}
				if got != want {
					t.Errorf("raw=%t snippet = %q, want %q", raw, got, want)
				}
			}
		})
	}
}
