package codec_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	upstream "go.yaml.in/yaml/v4"
)

type namedTargetRoot struct {
	Hook string `yaml:"hook"`
}

type namedTargetElement struct {
	Hook    string `yaml:"hook"`
	Enabled bool   `yaml:"enabled"`
}

type namedTargetMap map[string]namedTargetElement
type namedTargetSlice []namedTargetElement

func TestCursorErrorsNameRootType(t *testing.T) {
	var got namedTargetRoot
	err := decodeCursorSource([]byte("scalar\n"), &got)
	if err == nil {
		t.Fatal("scalar into named struct succeeded")
	}
	const want = "yaml: construct errors: line 1: cannot construct !!str `scalar` into codec_test.namedTargetRoot"
	if err.Error() != want {
		t.Errorf("root error = %q, want %q", err.Error(), want)
	}
}

func TestCursorErrorsNameMapAndSliceElementTypes(t *testing.T) {
	t.Run("map", func(t *testing.T) {
		var got namedTargetMap
		err := decodeCursorSource([]byte("item: scalar\n"), &got)
		if err == nil {
			t.Fatal("scalar into named map element succeeded")
		}
		const want = "yaml: construct errors: line 1: cannot construct !!str `scalar` into codec_test.namedTargetElement"
		if err.Error() != want {
			t.Errorf("map error = %q, want %q", err.Error(), want)
		}
	})
	t.Run("slice", func(t *testing.T) {
		var got namedTargetSlice
		err := decodeCursorSource([]byte("- scalar\n"), &got)
		if err == nil {
			t.Fatal("scalar into named slice element succeeded")
		}
		const want = "yaml: construct errors: line 1: cannot construct !!str `scalar` into codec_test.namedTargetElement"
		if err.Error() != want {
			t.Errorf("slice error = %q, want %q", err.Error(), want)
		}
	})
}

func TestSessionFinishJoinedDiagnosticsKeepMarksAndOrder(t *testing.T) {
	var session codec.Session
	err := session.Finish(fmt.Errorf("wrapped schema diagnostics: %w", errors.Join(
		&codec.LoadError{Message: "later", Mark: codec.Mark{Line: 2, Column: 1}},
		&codec.DecodeError{Errors: []*codec.LoadError{
			{Message: "late same line", Mark: codec.Mark{Line: 1, Column: 8}},
			{Message: "unknown"},
			{Message: "early same line", Mark: codec.Mark{Line: 1, Column: 4}},
		}},
	)))
	decoded, ok := errors.AsType[*codec.DecodeError](err)
	if !ok {
		t.Fatalf("schema error = %T %v, want *codec.DecodeError", err, err)
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
	_, codecErr := codec.Parse([]byte(source))
	var want upstream.Node
	upstreamErr := upstream.Unmarshal([]byte(source), &want)
	gotLoad, gotOK := errors.AsType[*codec.LoadError](codecErr)
	wantLoad, wantOK := errors.AsType[*upstream.LoadError](upstreamErr)
	if !gotOK || !wantOK {
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

type namedSelectedRoot struct {
	Hook codec.Selected `yaml:"hook"`
}
type namedSelectedMapRoot struct {
	Hook  codec.Selected `yaml:"hook"`
	Items namedTargetMap `yaml:"items"`
}
type namedSelectedSliceRoot struct {
	Hook  codec.Selected   `yaml:"hook"`
	Items namedTargetSlice `yaml:"items"`
}

func TestSelectErrorsNameCallerTypes(t *testing.T) {
	for _, tc := range []struct {
		name, source, want string
		target             any
		visits             int
	}{
		{"root", "scalar\n", "yaml: construct errors: line 1: cannot construct !!str `scalar` into codec_test.namedSelectedRoot", &namedSelectedRoot{}, 0},
		{"map element", "hook: seen\nitems: {item: scalar}\n", "yaml: construct errors: line 2: cannot construct !!str `scalar` into codec_test.namedTargetElement", &namedSelectedMapRoot{}, 1},
		{"slice element", "hook: seen\nitems: [scalar]\n", "yaml: construct errors: line 2: cannot construct !!str `scalar` into codec_test.namedTargetElement", &namedSelectedSliceRoot{}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, err := codec.Parse([]byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			session, cursor, err := codec.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			visits := 0
			err = cursor.Select(tc.target, func(_ *codec.Selected, value codec.Cursor) error {
				visits++
				var text string
				return value.Decode(&text)
			})
			err = session.Finish(err)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("error=%v want %q", err, tc.want)
			}
			for _, detail := range []string{"codec.Selected", "OccurrenceTarget", "struct { _"} {
				if strings.Contains(err.Error(), detail) {
					t.Errorf("error leaks %q: %v", detail, err)
				}
			}
			if visits != tc.visits {
				t.Errorf("visitor calls=%d want %d", visits, tc.visits)
			}
		})
	}
}

func TestSelectWrappedJoinedDiagnosticsKeepMarksOrderTextAndCauses(t *testing.T) {
	root, err := codec.Parse([]byte("ignored: ok\nother: ok\nhook: value\n"))
	if err != nil {
		t.Fatal(err)
	}
	session, cursor, err := codec.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	later := &codec.LoadError{Message: "later", Mark: codec.Mark{Line: 2, Column: 1}}
	late := &codec.LoadError{Message: "late same line", Mark: codec.Mark{Line: 1, Column: 8}}
	unknown := &codec.LoadError{Message: "unknown"}
	early := &codec.LoadError{Message: "early same line", Mark: codec.Mark{Line: 1, Column: 4}}
	joined := &codec.DecodeError{Errors: []*codec.LoadError{late, unknown, early}}
	wrapped := fmt.Errorf("wrapped schema diagnostics: %w", errors.Join(later, joined))
	var target namedSelectedRoot
	visits := 0
	err = cursor.Select(&target, func(*codec.Selected, codec.Cursor) error { visits++; return wrapped })
	err = session.Finish(err)
	if visits != 1 {
		t.Fatalf("visitor calls=%d want 1", visits)
	}
	decoded, ok := errors.AsType[*codec.DecodeError](err)
	if !ok || len(decoded.Errors) != 4 {
		t.Fatalf("diagnostics=%v want 4", err)
	}
	wantMarks := []codec.Mark{{Line: 1, Column: 4}, {Line: 1, Column: 8}, {Line: 2, Column: 1}, {Line: 3, Column: 7}}
	wantMessages := []string{"early same line", "late same line", "later", "unknown"}
	for i, item := range decoded.Errors {
		if item.Mark != wantMarks[i] || item.Message != "wrapped schema diagnostics: "+wantMessages[i] {
			t.Errorf("diagnostic %d=%+v", i, item)
		}
	}
	const want = "yaml: construct errors: line 1: wrapped schema diagnostics: early same line; line 1: wrapped schema diagnostics: late same line; line 2: wrapped schema diagnostics: later; line 3: wrapped schema diagnostics: unknown"
	if err.Error() != want {
		t.Errorf("text=%q want %q", err.Error(), want)
	}
	for _, cause := range []error{later, late, unknown, early, wrapped} {
		if !errors.Is(err, cause) {
			t.Errorf("lost original cause %v", cause)
		}
	}
	if unknown.Mark != (codec.Mark{}) || unknown.Message != "unknown" {
		t.Error("Wrap mutated original unknown diagnostic")
	}
}
