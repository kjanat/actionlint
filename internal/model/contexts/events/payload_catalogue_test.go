package events

import "testing"

func TestCatalogueReferences(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	variants, ok := PayloadVariants("push")
	if !ok {
		t.Fatal("missing push observation")
	}
	var id int
	for _, value := range variants {
		id = value
		break
	}
	clear(variants)
	again, _ := PayloadVariants("push")
	if len(again) == 0 {
		t.Fatal("caller changed catalogue variants")
	}
	node := PayloadNode(id)
	for node.Kind == "intersection" && len(node.Alternatives) == 1 {
		id = node.Alternatives[0]
		node = PayloadNode(id)
	}
	if len(node.Properties) == 0 {
		t.Fatal("push has no properties")
	}
	clear(node.Properties)
	if len(PayloadNode(id).Properties) == 0 {
		t.Fatal("caller changed catalogue properties")
	}
}
