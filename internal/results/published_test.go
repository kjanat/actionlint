package results

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
)

func TestPublishResolvesEndSnippetAndExcerptOnce(t *testing.T) {
	t.Parallel()
	at := func(line, column int) sourcetext.Position { return sourcetext.Position{Line: line, Column: column} }
	unavailable := ReportedSourceUnavailable("tool", "reported.sh", false)
	for _, tc := range []struct {
		name    string
		d       Diagnostic
		source  DiagnosticSource
		end     sourcetext.Position
		snippet string
		excerpt Excerpt
	}{
		{"token end inferred", Diagnostic{Start: at(1, 5)}, DiagnosticSource{Content: []byte("abc def\n")}, at(1, 8), "abc def", Excerpt{ExcerptText, "abc def", 7}},
		{"reported end kept", Diagnostic{Start: at(1, 1), End: at(1, 3)}, DiagnosticSource{Content: []byte("abc def\n")}, at(1, 3), "abc def", Excerpt{ExcerptText, "abc def", 2}},
		{"captured snippet without source", Diagnostic{Start: at(2, 1), Snippet: "captured"}, DiagnosticSource{}, at(2, 2), "captured", Excerpt{ExcerptText, "captured", 1}},
		{"start outside source", Diagnostic{Start: at(5, 1), Snippet: "captured"}, DiagnosticSource{Content: []byte("a\n")}, at(5, 2), "captured", Excerpt{}},
		{"empty source", Diagnostic{Start: at(1, 1)}, DiagnosticSource{Content: []byte{}}, at(1, 2), "", Excerpt{ExcerptEmptySource, "", 1}},
		{"no source or snippet", Diagnostic{Start: at(1, 1)}, DiagnosticSource{}, at(1, 2), "", Excerpt{ExcerptEmptySource, "", 1}},
		{"no start line", Diagnostic{}, DiagnosticSource{Content: []byte("a\n")}, at(0, 1), "", Excerpt{}},
		{"unavailable point", Diagnostic{Start: at(1, 6)}, DiagnosticSource{Content: []byte("echo long_token\n"), RawScript: true, Unavailable: unavailable}, at(1, 6), "echo long_token", Excerpt{ExcerptText, "echo long_token", 6}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Publish(tc.d, tc.source)
			if got.End != tc.end || got.Snippet != tc.snippet || got.Excerpt != tc.excerpt {
				t.Fatalf("end=%+v snippet=%q excerpt=%+v; want end=%+v snippet=%q excerpt=%+v", got.End, got.Snippet, got.Excerpt, tc.end, tc.snippet, tc.excerpt)
			}
		})
	}
}

func TestDiagnosticsKeepsNilAndOrder(t *testing.T) {
	t.Parallel()
	if Diagnostics(nil) != nil {
		t.Fatal("nil publication became an empty list")
	}
	got := Diagnostics([]PublishedDiagnostic{{Message: "first"}, {Message: "second"}})
	if len(got) != 2 || got[0].Message != "first" || got[1].Message != "second" {
		t.Fatalf("projection lost order: %+v", got)
	}
	if empty := Diagnostics([]PublishedDiagnostic{}); empty == nil || len(empty) != 0 {
		t.Fatal("empty publication became nil")
	}
}
