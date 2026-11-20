package main

import (
	"go/build"
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/plugins/sdk/go/bindings/kjanat/actionlint/v0.2.0/analyzer"
	"go.bytecodealliance.org/cm"
)

func TestGuestBuildExcludesNativePackaging(t *testing.T) {
	for _, test := range []struct {
		name, architecture string
		tags               []string
		guest              bool
	}{
		{name: "native Go", architecture: build.Default.GOARCH},
		{name: "TinyGo host architecture", architecture: build.Default.GOARCH, tags: []string{"tinygo"}, guest: true},
		{name: "Wasm architecture", architecture: "wasm", guest: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			context := build.Default
			context.GOARCH, context.BuildTags = test.architecture, test.tags
			guest, err := context.MatchFile(".", "main_guest.go")
			if err != nil {
				t.Fatal(err)
			}
			native, err := context.MatchFile(".", "main_native.go")
			if err != nil {
				t.Fatal(err)
			}
			if guest != test.guest || native == test.guest {
				t.Fatalf("guest selected=%t, native selected=%t; want guest=%t", guest, native, test.guest)
			}
		})
	}
}

func TestAnalyzeUsesMappedEndOfInsecureFlag(t *testing.T) {
	t.Parallel()
	start := analyzer.Position{Line: 7, Column: 15}
	end := analyzer.Position{Line: 7, Column: 22}
	script := analyzer.Script{
		Path: "action.yml",
		Text: "curl -k https://example.com",
		Spans: cm.ToList([]analyzer.Span{{
			Line: 1, Start: 6, End: 8, Document: start, DocumentEnd: cm.Some(end),
		}}),
	}
	got := analyze(analyzer.Input{Invoked: cm.ToList([]analyzer.Script{script})}).Diagnostics.Slice()
	if len(got) != 1 || got[0].Path != script.Path || got[0].Start != start || got[0].End != end {
		t.Fatalf("diagnostics = %+v, want one finding in %q from %+v to %+v", got, script.Path, start, end)
	}
}

func TestInsecureDownloadFlags(t *testing.T) {
	for _, test := range []struct {
		source string
		want   []token
	}{
		{"curl --insecure https://example.com", []token{{6, "--insecure"}}},
		{"curl -fsSk https://example.com", []token{{6, "-fsSk"}}},
		{"wget --no-check-certificate https://example.com", []token{{6, "--no-check-certificate"}}},
		{"curl --fail https://example.com", nil},
		{"echo --insecure", nil},
	} {
		t.Run(test.source, func(t *testing.T) {
			if got := insecureFlags(test.source); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("flags=%+v, want %+v", got, test.want)
			}
		})
	}
}
