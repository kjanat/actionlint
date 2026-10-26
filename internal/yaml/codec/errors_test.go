package codec

import (
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

type errorHook struct{}

func (*errorHook) UnmarshalYAML(_ *Decoder, _ *syntax.Node) error { return errors.New("hook rejected") }

func TestDecodeErrorsHaveStableSourceOrder(t *testing.T) {
	var target struct {
		Early errorHook `yaml:"early"`
		Bad   bool      `yaml:"bad"`
		Late  errorHook `yaml:"late"`
	}
	err := Unmarshal([]byte("early: one\nbad: []\nlate: two\n"), &target)
	list, ok := errors.AsType[*DecodeError](err)
	if !ok || len(list.Errors) != 3 {
		t.Fatalf("decode error = %#v", err)
	}
	for i, position := range []Mark{{Line: 1, Column: 8}, {Line: 2, Column: 6}, {Line: 3, Column: 7}} {
		if got := list.Errors[i].Mark; got.Line != position.Line || got.Column != position.Column {
			t.Fatalf("error %d position = %+v, want %+v", i, got, position)
		}
	}
	const want = "yaml: construct errors: line 1: hook rejected; line 2: cannot construct !!seq into bool; line 3: hook rejected"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestDecodeErrorFormatIndependentOfTargetHooks(t *testing.T) {
	var direct struct {
		Bad bool `yaml:"bad"`
	}
	var shadow struct {
		Bad  bool      `yaml:"bad"`
		Hook errorHook `yaml:"hook"`
	}
	first := Unmarshal([]byte("bad: []\n"), &direct)
	second := Unmarshal([]byte("bad: []\n"), &shadow)
	const want = "yaml: construct errors: line 1: cannot construct !!seq into bool"
	if first == nil || second == nil || first.Error() != want || second.Error() != want {
		t.Fatalf("direct = %v; shadow = %v", first, second)
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

func TestEmptyInputValidatesTargetsAndSkipsHooks(t *testing.T) {
	for _, source := range []string{"", "# comment\n"} {
		if err := Unmarshal([]byte(source), (*syntax.Node)(nil)); err == nil {
			t.Fatal("empty stream accepted a nil target")
		}
	}
	if err := Decode(&syntax.Node{Kind: syntax.DocumentNode}, &errorHook{}); err != nil {
		t.Fatalf("empty document invoked hook: %v", err)
	}
}
