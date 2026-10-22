package documentparse

import (
	"testing"

	"actionlint.kjanat.dev/internal/yaml"
)

func TestCompositeScriptNodeSharing(t *testing.T) {
	leaf := &yaml.Node{Kind: yaml.ScalarNode, Value: "hello", Line: 3, Column: 5}
	shared := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{leaf}}
	alias := &yaml.Node{Kind: yaml.AliasNode, Alias: shared}
	root := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{shared, alias, alias}}
	clone, ok := cloneActionNode(root, make(map[*yaml.Node]bool), make(map[*yaml.Node]*yaml.Node))
	if !ok || clone == root || len(clone.Content) != 3 {
		t.Fatalf("clone failed: %v, %+v", ok, clone)
	}
	if clone.Content[0] == shared || clone.Content[0] != clone.Content[1] || clone.Content[1] != clone.Content[2] {
		t.Fatal("repeated aliases must reuse the completed clone")
	}
	if got := clone.Content[0].Content[0]; got == leaf || got.Value != leaf.Value || got.Line != leaf.Line || got.Column != leaf.Column {
		t.Fatalf("source information lost: %+v", got)
	}
	if root.Content[1] != alias || alias.Kind != yaml.AliasNode || alias.Alias != shared {
		t.Fatal("shared metadata was mutated")
	}
}

func TestCompositeScriptNodeCycle(t *testing.T) {
	root := &yaml.Node{Kind: yaml.SequenceNode}
	root.Content = []*yaml.Node{{Kind: yaml.AliasNode, Alias: root}}
	done := make(map[*yaml.Node]*yaml.Node)
	if _, ok := cloneActionNode(root, make(map[*yaml.Node]bool), done); ok {
		t.Fatal("accepted a cyclic node graph")
	}
	if _, ok := done[root]; ok {
		t.Fatal("cached an incomplete clone")
	}
}
