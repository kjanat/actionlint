package codec_test

import (
	"errors"
	"fmt"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

func TestCursorWrapLocatesEachErrorWithoutChangingCauses(t *testing.T) {
	node := &syntax.Node{Kind: syntax.ScalarNode, Tag: "!!str", Value: "value", Line: 4, Column: 3}
	session, _, err := codec.Open(node)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	plain := errors.New("invalid value")
	located := codec.NewLoadError("duplicate key", codec.Mark{Line: 9, Column: 7})
	unknown := codec.NewLoadError("missing property", codec.Mark{})
	for _, wrapped := range []bool{false, true} {
		input := errors.Join(plain, located, unknown)
		prefix := ""
		if wrapped {
			input = fmt.Errorf("section: %w", input)
			prefix = "section: "
		}
		got := session.Wrap(node, input)
		decoded, ok := errors.AsType[*codec.DecodeError](got)
		if !ok || len(decoded.Errors) != 3 {
			t.Fatalf("Wrap = %v, want three located errors", got)
		}
		for i, want := range []struct {
			message string
			mark    codec.Mark
		}{
			{prefix + "invalid value", codec.Mark{Line: 4, Column: 3}},
			{prefix + "missing property", codec.Mark{Line: 4, Column: 3}},
			{prefix + "duplicate key", codec.Mark{Line: 9, Column: 7}},
		} {
			if decoded.Errors[i].Message() != want.message || decoded.Errors[i].Mark() != want.mark {
				t.Errorf("error %d = %+v, want %+v", i, decoded.Errors[i], want)
			}
		}
		for _, cause := range []error{plain, located, unknown} {
			if !errors.Is(got, cause) {
				t.Errorf("lost cause %v", cause)
			}
		}
	}
	if unknown.Mark() != (codec.Mark{}) || located.Mark() != (codec.Mark{Line: 9, Column: 7}) || located.Message() != "duplicate key" {
		t.Fatal("Wrap changed an original diagnostic")
	}
}

func TestCursorWrapWithoutNodeReportsUnknownLocation(t *testing.T) {
	var session codec.Session
	cause := errors.New("missing node")
	err := session.Wrap(nil, cause)
	decoded, ok := errors.AsType[*codec.DecodeError](err)
	if !ok || len(decoded.Errors) != 1 || decoded.Errors[0].Mark() != (codec.Mark{}) || !errors.Is(err, cause) {
		t.Fatalf("Wrap(nil) = %v, want unlocated diagnostic with its cause", err)
	}
	if err := session.Wrap(nil, nil); err != nil {
		t.Fatalf("Wrap(nil, nil) = %v", err)
	}
}
