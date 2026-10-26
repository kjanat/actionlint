package documentparse

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

func TestCompositeScriptNodeSharing(t *testing.T) {
	leaf := &syntax.Node{Kind: syntax.ScalarNode, Value: "hello", Line: 3, Column: 5}
	shared := &syntax.Node{Kind: syntax.SequenceNode, Content: []*syntax.Node{leaf}}
	alias := &syntax.Node{Kind: syntax.AliasNode, Alias: shared}
	root := &syntax.Node{Kind: syntax.SequenceNode, Content: []*syntax.Node{shared, alias, alias}}
	clone, ok := cloneActionNode(root, make(map[*syntax.Node]bool), make(map[*syntax.Node]*syntax.Node))
	if !ok || clone == root || len(clone.Content) != 3 {
		t.Fatalf("clone failed: %v, %+v", ok, clone)
	}
	if clone.Content[0] == shared || clone.Content[0] != clone.Content[1] || clone.Content[1] != clone.Content[2] {
		t.Fatal("repeated aliases must reuse the completed clone")
	}
	if got := clone.Content[0].Content[0]; got == leaf || got.Value != leaf.Value || got.Line != leaf.Line || got.Column != leaf.Column {
		t.Fatalf("source information lost: %+v", got)
	}
	if root.Content[1] != alias || alias.Kind != syntax.AliasNode || alias.Alias != shared {
		t.Fatal("shared metadata was mutated")
	}
}

func TestCompositeScriptNodeCycle(t *testing.T) {
	root := &syntax.Node{Kind: syntax.SequenceNode}
	root.Content = []*syntax.Node{{Kind: syntax.AliasNode, Alias: root}}
	done := make(map[*syntax.Node]*syntax.Node)
	if _, ok := cloneActionNode(root, make(map[*syntax.Node]bool), done); ok {
		t.Fatal("accepted a cyclic node graph")
	}
	if _, ok := done[root]; ok {
		t.Fatal("cached an incomplete clone")
	}
}
