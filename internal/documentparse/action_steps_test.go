package documentparse

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

func TestRejectedCompositeRunsKeepSurroundingDeclarations(t *testing.T) {
	for _, invalid := range []string{"*runs", "[echo, bad]", "{command: bad}", "!unsupported bad"} {
		t.Run(invalid, func(t *testing.T) {
			content := []byte("name: test\ndescription: test\nruns: &runs\n  using: composite\n  steps:\n    - shell: bash\n      run: echo before\n    - shell: bash\n      run: " + invalid + "\n    - shell: bash\n      run: echo after\n")
			document := Action("action.yml", "action.yml", content)
			registry := newCapturedSnapshots(t, sourceFixtureBase(t))
			snapshot, err := registry.Document("action.yml", content, document)
			if err != nil {
				t.Fatal(err)
			}
			steps := snapshotActionSteps(t, snapshot)
			if len(steps) != 3 {
				t.Fatalf("recovered steps = %d", len(steps))
			}
			for index, want := range map[int]string{0: "echo before", 2: "echo after"} {
				run, ok := steps[index].Run()
				if !ok || !run.Run().Present() || run.Run().Value() != want || !run.Source().Present() {
					t.Fatalf("surrounding run %d lost its source: %+v", index, steps[index])
				}
			}
			if _, executable := steps[1].Run(); executable {
				t.Fatal("rejected composite run remained executable")
			}
		})
	}
}

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
