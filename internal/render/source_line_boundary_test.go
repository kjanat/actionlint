package render_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/render"
)

func TestTemplateSnippetLineEndings(t *testing.T) {
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
				d := diagnostic.Diagnostic{Start: diagnostic.Position{Line: 1}}
				canonical := (&findingmodel.Finding{Diagnostic: d, RawScriptSource: raw}).Canonical(source)
				view := render.FindingOf(canonical, source)
				got := view.TemplateFields().Snippet
				want := "token"
				if raw {
					want = "\ufefftoken" + ending.text + "tail"
					if ending.text == "\r\n" {
						want = "\ufefftoken"
					}
				}
				if got != want || got != canonical.Snippet {
					t.Errorf("raw=%t template snippet = %q, canonical = %q, want %q", raw, got, canonical.Snippet, want)
				}
			}
		})
	}
}
