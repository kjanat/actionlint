package codec

import (
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

func errorCursor(t *testing.T, data []byte) Cursor {
	t.Helper()
	root, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	session, cursor, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	return cursor
}

func TestDecodeErrorsHaveStableSourceOrder(t *testing.T) {
	var target struct {
		Early Selected `yaml:"early"`
		Bad   bool     `yaml:"bad"`
		Late  Selected `yaml:"late"`
	}
	cursor := errorCursor(t, []byte("early: one\nbad: []\nlate: two\n"))
	rejected := errors.New("schema rejected")
	err := cursor.Select(&target, func(*Selected, Cursor) error { return rejected })
	list, ok := errors.AsType[*DecodeError](err)
	if !ok || len(list.Errors) != 3 || !errors.Is(err, rejected) {
		t.Fatalf("decode error = %#v", err)
	}
	for i, position := range []Mark{{Line: 1, Column: 8}, {Line: 2, Column: 6}, {Line: 3, Column: 7}} {
		if got := list.Errors[i].Mark; got.Line != position.Line || got.Column != position.Column {
			t.Fatalf("error %d position = %+v, want %+v", i, got, position)
		}
	}
	const want = "yaml: construct errors: line 1: schema rejected; line 2: cannot construct !!seq into bool; line 3: schema rejected"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestDecodeErrorFormatIndependentOfSelectedFields(t *testing.T) {
	var direct struct {
		Bad bool `yaml:"bad"`
	}
	var selected struct {
		Bad   bool     `yaml:"bad"`
		Extra Selected `yaml:"extra"`
	}
	first := errorCursor(t, []byte("bad: []\n")).Decode(&direct)
	second := errorCursor(t, []byte("bad: []\n")).Select(&selected, func(*Selected, Cursor) error { t.Fatal("absent selected field visited"); return nil })
	const want = "yaml: construct errors: line 1: cannot construct !!seq into bool"
	if first == nil || second == nil || first.Error() != want || second.Error() != want {
		t.Fatalf("direct = %v; selected = %v", first, second)
	}
}

func TestJoinedDecodeErrorsPreserveForeignCauses(t *testing.T) {
	foreign := errors.New("foreign failure")
	located := &LoadError{Message: "located failure", Mark: Mark{Line: 2, Column: 1}}
	err := joinDecodeErrors(joinDecodeErrors(located, foreign), &LoadError{Message: "later", Mark: Mark{Line: 3}})
	list, ok := errors.AsType[*DecodeError](err)
	if !ok || len(list.Errors) != 3 || !errors.Is(err, foreign) || !errors.Is(err, located) {
		t.Fatalf("lost cause or diagnostic: %#v", err)
	}
}

func TestEmptyInputValidatesTargets(t *testing.T) {
	for _, source := range []string{"", "# comment\n"} {
		cursor := errorCursor(t, []byte(source))
		if err := cursor.Decode((*syntax.Node)(nil)); err == nil {
			t.Fatal("empty stream accepted a nil target")
		}
		value := "preserved"
		if err := cursor.Decode(&value); err != nil || value != "preserved" {
			t.Fatalf("empty input changed target: value=%q err=%v", value, err)
		}
	}
}
