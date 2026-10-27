package codec

import (
	"errors"
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

func TestOwnedGraphAndNestedDecode(t *testing.T) {
	root, err := Parse([]byte("base: &a {value: null}\ncopy: *a\n"))
	if err != nil {
		t.Fatal(err)
	}
	mapping := root.Content[0]
	if mapping.Content[3].Alias != mapping.Content[1] {
		t.Fatal("alias identity lost")
	}
	mapping.Content[1].Content[1].HeadComment = "changed"
	session, cursor, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var target struct {
		Base Selected `yaml:"base"`
		Copy Selected `yaml:"copy"`
	}
	visits, children := 0, 0
	err = cursor.Select(&target, func(_ *Selected, occurrence Cursor) error {
		visits++
		entered, err := occurrence.Enter()
		if err != nil {
			return err
		}
		if entered.Node() != mapping.Content[1] {
			t.Errorf("entered=%p anchor=%p", entered.Node(), mapping.Content[1])
		}
		var fields struct {
			Value Selected `yaml:"value"`
		}
		return entered.Select(&fields, func(_ *Selected, child Cursor) error {
			children++
			value, err := child.Enter()
			if err != nil {
				return err
			}
			node := value.Node()
			if node != mapping.Content[1].Content[1] || node.Tag != "!!null" || node.Line != 1 || node.HeadComment != "changed" {
				t.Errorf("entered null child=%#v", node)
			}
			return nil
		})
	})
	if err := session.Finish(err); err != nil {
		t.Fatal(err)
	}
	if visits != 2 || children != 2 {
		t.Errorf("base/copy visits=%d child visits=%d want 2 each", visits, children)
	}
	if reflect.TypeFor[syntax.Node]().PkgPath() != "actionlint.kjanat.dev/v2/internal/yaml/syntax" {
		t.Fatal("tree is not owned by syntax")
	}
}

func TestPartialDecodeAndErrors(t *testing.T) {
	var result struct {
		A string `yaml:"a"`
		B bool   `yaml:"b"`
	}
	cursor := errorCursor(t, []byte("a: stored\nb: []\n"))
	err := cursor.Decode(&result)
	if err == nil || result.A != "stored" {
		t.Fatalf("%+v %v", result, err)
	}
	decoded, ok := errors.AsType[*DecodeError](err)
	if !ok || len(decoded.Errors) != 1 || decoded.Errors[0].Mark.Line != 2 || decoded.Errors[0].Mark.Column != 4 {
		t.Fatalf("decode error = %#v, want type error at 2:4", err)
	}
}
