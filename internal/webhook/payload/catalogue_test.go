package payload

import "testing"

func TestCatalogueReferences(t *testing.T) {
	c := data()
	if len(c.Events) < 70 {
		t.Fatalf("unexpected event count: %d", len(c.Events))
	}
	for i, n := range c.Nodes {
		refs := append([]int{}, n.Alternatives...)
		if n.Kind == "array" {
			refs = append(refs, n.Element)
		}
		if n.Mapped != nil {
			refs = append(refs, *n.Mapped)
		}
		for _, p := range n.Properties {
			refs = append(refs, p.Type)
		}
		for _, ref := range refs {
			if ref < 0 || ref >= len(c.Nodes) {
				t.Errorf("node %d: invalid reference %d", i, ref)
			}
		}
	}
	for event, variants := range c.Events {
		for action, id := range variants {
			if id < 0 || id >= len(c.Nodes) {
				t.Errorf("%s/%s: invalid reference %d", event, action, id)
			}
		}
	}
}

func TestObservationsAreIsolated(t *testing.T) {
	variants, ok := Variants("push")
	if !ok {
		t.Fatal("missing push observation")
	}
	var id int
	for _, value := range variants {
		id = value
		break
	}
	clear(variants)
	again, _ := Variants("push")
	if len(again) == 0 {
		t.Fatal("caller changed catalogue variants")
	}
	node := Node(id)
	if len(node.Properties) == 0 {
		t.Fatal("push has no properties")
	}
	clear(node.Properties)
	if len(Node(id).Properties) == 0 {
		t.Fatal("caller changed catalogue properties")
	}
}
