package codec

import (
	"errors"
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

type custom struct{ Value string }

func (c *custom) UnmarshalYAML(_ *Decoder, n *syntax.Node) error { c.Value = n.Value; return nil }

func TestOwnedGraphAndNestedDecode(t *testing.T) {
	var root syntax.Node
	if err := Unmarshal([]byte("base: &a {value: null}\ncopy: *a\n"), &root); err != nil {
		t.Fatal(err)
	}
	mapping := root.Content[0]
	if mapping.Content[3].Alias != mapping.Content[1] {
		t.Fatal("alias identity lost")
	}
	mapping.Content[1].Content[1].HeadComment = "changed"
	var value struct {
		Base struct{ Value custom } `yaml:"base"`
		Copy struct{ Value custom } `yaml:"copy"`
	}
	if err := Decode(&root, &value); err != nil {
		t.Fatal(err)
	}
	if value.Base.Value.Value != "" {
		t.Fatal(value)
	}
	var nodes struct {
		Base syntax.Node `yaml:"base"`
	}
	if err := Decode(&root, &nodes); err != nil {
		t.Fatal(err)
	}
	if nodes.Base.Content[1].Tag != "!!null" || nodes.Base.Content[1].Line != 1 {
		t.Fatal(nodes)
	}
	if nodes.Base.Content[1].HeadComment != "changed" {
		t.Fatal("source comment lost during node decoding")
	}
	if reflect.TypeFor[syntax.Node]().PkgPath() != "actionlint.kjanat.dev/v2/internal/yaml/syntax" {
		t.Fatal("tree is not owned by syntax")
	}
}

func TestPartialDecodeAndErrors(t *testing.T) {
	var result struct {
		A custom `yaml:"a"`
		B bool   `yaml:"b"`
	}
	err := Unmarshal([]byte("a: retained\nb: []\n"), &result)
	if err == nil || result.A.Value != "retained" {
		t.Fatalf("%+v %v", result, err)
	}
	decoded, ok := errors.AsType[*DecodeError](err)
	if !ok || len(decoded.Errors) != 1 || decoded.Errors[0].Mark.Line != 2 || decoded.Errors[0].Mark.Column != 4 {
		t.Fatalf("decode error = %#v, want type error at 2:4", err)
	}
}
