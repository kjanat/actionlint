package codec_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
	upstream "go.yaml.in/yaml/v4"
)

type namedShadowRoot struct {
	Hook codecNodeHook `yaml:"hook"`
}

type namedShadowElement struct {
	Hook    codecNodeHook `yaml:"hook"`
	Enabled bool          `yaml:"enabled"`
}

type namedShadowMap map[string]namedShadowElement
type namedShadowSlice []namedShadowElement

func TestShadowErrorsNameOriginalRootType(t *testing.T) {
	var got namedShadowRoot
	err := codec.Unmarshal([]byte("scalar\n"), &got)
	if err == nil {
		t.Fatal("scalar into named shadow struct succeeded")
	}
	const want = "yaml: construct errors: line 1: cannot construct !!str `scalar` into codec_test.namedShadowRoot"
	if err.Error() != want {
		t.Errorf("root error = %q, want %q", err.Error(), want)
	}
	assertNoShadowDetails(t, err)
}

func TestShadowErrorsNameOriginalMapAndSliceElementTypes(t *testing.T) {
	t.Run("map", func(t *testing.T) {
		var got namedShadowMap
		err := codec.Unmarshal([]byte("item: scalar\n"), &got)
		if err == nil {
			t.Fatal("scalar into named shadow map element succeeded")
		}
		const want = "yaml: construct errors: line 1: cannot construct !!str `scalar` into codec_test.namedShadowElement"
		if err.Error() != want {
			t.Errorf("map error = %q, want %q", err.Error(), want)
		}
		assertNoShadowDetails(t, err)
	})
	t.Run("slice", func(t *testing.T) {
		var got namedShadowSlice
		err := codec.Unmarshal([]byte("- scalar\n"), &got)
		if err == nil {
			t.Fatal("scalar into named shadow slice element succeeded")
		}
		const want = "yaml: construct errors: line 1: cannot construct !!str `scalar` into codec_test.namedShadowElement"
		if err.Error() != want {
			t.Errorf("slice error = %q, want %q", err.Error(), want)
		}
		assertNoShadowDetails(t, err)
	})
}

func assertNoShadowDetails(t *testing.T, err error) {
	t.Helper()
	for _, detail := range []string{"codec.captured", "CodecOriginal", "CodecEmbedded", "parsedSource", "positions"} {
		if strings.Contains(err.Error(), detail) {
			t.Errorf("error leaks shadow detail %q: %v", detail, err)
		}
	}
}

type nestedDiagnosticHook struct{}

func (*nestedDiagnosticHook) UnmarshalYAML(_ *codec.Decoder, _ *syntax.Node) error {
	return fmt.Errorf("wrapped hook diagnostics: %w", errors.Join(
		&codec.LoadError{Message: "later", Mark: codec.Mark{Line: 2, Column: 1}},
		&codec.DecodeError{Errors: []*codec.LoadError{
			{Message: "late same line", Mark: codec.Mark{Line: 1, Column: 8}},
			{Message: "unknown", Mark: codec.Mark{}},
			{Message: "early same line", Mark: codec.Mark{Line: 1, Column: 4}},
		}},
	))
}

func TestWrappedJoinedHookDiagnosticsKeepMarksAndOrder(t *testing.T) {
	var got nestedDiagnosticHook
	err := codec.Unmarshal([]byte("hook: value\n"), &got)
	var decoded *codec.DecodeError
	if !errors.As(err, &decoded) {
		t.Fatalf("hook error = %T %v, want *codec.DecodeError", err, err)
	}
	wantMarks := []codec.Mark{{}, {Line: 1, Column: 4}, {Line: 1, Column: 8}, {Line: 2, Column: 1}}
	wantMessages := []string{"unknown", "early same line", "late same line", "later"}
	if len(decoded.Errors) != len(wantMarks) {
		t.Fatalf("diagnostics = %#v, want %d original errors", decoded.Errors, len(wantMarks))
	}
	for i, item := range decoded.Errors {
		if item.Mark != wantMarks[i] || item.Message != wantMessages[i] {
			t.Errorf("diagnostic %d = (%+v, %q), want (%+v, %q)", i, item.Mark, item.Message, wantMarks[i], wantMessages[i])
		}
	}
	const wantText = "yaml: construct errors: <unknown position>: unknown; line 1: early same line; line 1: late same line; line 2: later"
	if err.Error() != wantText {
		t.Errorf("joined diagnostics = %q, want %q", err.Error(), wantText)
	}
}

func TestParserErrorMatchesPinnedUpstreamGolden(t *testing.T) {
	const source = "value: [\n"
	var got syntax.Node
	codecErr := codec.Unmarshal([]byte(source), &got)
	var want upstream.Node
	upstreamErr := upstream.Unmarshal([]byte(source), &want)
	var gotLoad *codec.LoadError
	var wantLoad *upstream.LoadError
	if !errors.As(codecErr, &gotLoad) || !errors.As(upstreamErr, &wantLoad) {
		t.Fatalf("codec parser error = %T %v; upstream parser error = %T %v", codecErr, codecErr, upstreamErr, upstreamErr)
	}
	if gotLoad.Error() != wantLoad.Error() || gotLoad.Mark.Index != wantLoad.Mark.Index || gotLoad.Mark.Line != wantLoad.Mark.Line || gotLoad.Mark.Column != wantLoad.Mark.Column {
		t.Errorf("codec parser error (%q, %+v) differs from upstream (%q, %+v)", gotLoad.Error(), gotLoad.Mark, wantLoad.Error(), wantLoad.Mark)
	}
	const wantError = "go-yaml load error in parser (while parsing a flow node) at L2.C1: did not find expected node content"
	wantMark := codec.Mark{Index: 9, Line: 2, Column: 1}
	if gotLoad.Error() != wantError || gotLoad.Mark != wantMark {
		t.Errorf("parser error = (%q, %+v), want (%q, %+v)", gotLoad.Error(), gotLoad.Mark, wantError, wantMark)
	}
}
