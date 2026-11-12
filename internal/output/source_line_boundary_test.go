package output_test

import (
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	render "actionlint.kjanat.dev/v2/internal/output"
	"actionlint.kjanat.dev/v2/internal/results"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

func TestTemplateSnippetLineEndings(t *testing.T) {
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
				d := results.Diagnostic{Start: sourcepos.Position{Line: 1}}
				canonical := (&findingmodel.Finding{Diagnostic: d, RawScriptSource: raw}).Canonical(source)
				view := render.FindingOf(canonical, source)
				view.RawScript = raw
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

func TestRawCRLFMarkerUsesDisplayEOF(t *testing.T) {
	t.Parallel()
	source := []byte("abc\r\n")
	d := results.Diagnostic{Start: sourcepos.Position{Line: 1, Column: 4}, End: sourcepos.Position{Line: 1, Column: 5}}
	canonical := (&findingmodel.Finding{Diagnostic: d, RawScriptSource: true}).Canonical(source)
	view := render.FindingOf(canonical, source)
	view.RawScript = true
	if got := view.TemplateFields().Snippet; got != "abc\n   ^" {
		t.Fatalf("CR marker lost at display EOF: %q", got)
	}
	if canonical.Start != d.Start || canonical.End != d.End {
		t.Fatalf("display normalization changed physical coordinates: %+v", canonical)
	}
}
