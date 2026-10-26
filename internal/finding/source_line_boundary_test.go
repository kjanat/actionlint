package finding

import (
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
)

func TestCanonicalSnippetLineEndings(t *testing.T) {
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
			source := []byte("\ufefftoken" + ending.text + "tail")
			for _, raw := range []bool{false, true} {
				finding := &Finding{Start: diagnostic.Position{Line: 1}, RawScriptSource: raw}
				got := finding.Canonical(source).Snippet
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
