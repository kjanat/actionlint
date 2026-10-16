package yaml

import (
	"reflect"
	"testing"
)

type custom struct{ Value string }

func (c *custom) UnmarshalYAML(n *Node) error { c.Value = n.Value; return nil }
func TestOwnedGraphAndNestedDecode(t *testing.T) {
	var root Node
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
	if err := root.Decode(&value); err != nil {
		t.Fatal(err)
	}
	// The explicit null remains a null regardless of the display value.
	if value.Base.Value.Value != "" {
		t.Fatal(value)
	}
	var nodes struct {
		Base Node `yaml:"base"`
	}
	if err := root.Decode(&nodes); err != nil {
		t.Fatal(err)
	}
	if nodes.Base.Content[1].Tag != "!!null" || nodes.Base.Content[1].Line != 1 {
		t.Fatal(nodes)
	}
	if reflect.TypeFor[Node]().PkgPath() != "actionlint.kjanat.dev/internal/yaml" {
		t.Fatal("tree is a parser alias")
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
}
