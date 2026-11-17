package parsing

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
)

func TestRejectedCompositeRunsKeepSurroundingDeclarations(t *testing.T) {
	t.Parallel()
	for _, invalid := range []string{"*runs", "[echo, bad]", "{command: bad}", "!unsupported bad"} {
		t.Run(invalid, func(t *testing.T) {
			t.Parallel()
			content := []byte("name: test\ndescription: test\nruns: &runs\n  using: composite\n  steps:\n    - shell: bash\n      run: echo before\n    - shell: bash\n      run: " + invalid + "\n    - shell: bash\n      run: echo after\n")
			document := Action("action.yml", content)
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
				if !ok || run.Run == nil || run.Run.Text() != want || run.Source == nil {
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
	t.Parallel()
	leaf := &codec.Node{Kind: codec.ScalarNode, Value: "hello", Line: 3, Column: 5}
	shared := &codec.Node{Kind: codec.SequenceNode, Content: []*codec.Node{leaf}}
	alias := &codec.Node{Kind: codec.AliasNode, Alias: shared}
	root := &codec.Node{Kind: codec.SequenceNode, Content: []*codec.Node{shared, alias, alias}}
	clone, ok := cloneActionNode(root, make(map[*codec.Node]bool), make(map[*codec.Node]*codec.Node))
	if !ok || clone == root || len(clone.Content) != 3 {
		t.Fatalf("clone failed: %v, %+v", ok, clone)
	}
	if clone.Content[0] == shared || clone.Content[0] != clone.Content[1] || clone.Content[1] != clone.Content[2] {
		t.Fatal("repeated aliases must reuse the completed clone")
	}
	if got := clone.Content[0].Content[0]; got == leaf || got.Value != leaf.Value || got.Line != leaf.Line || got.Column != leaf.Column {
		t.Fatalf("source information lost: %+v", got)
	}
	if root.Content[1] != alias || alias.Kind != codec.AliasNode || alias.Alias != shared {
		t.Fatal("shared metadata was mutated")
	}
}

func TestCompositeScriptNodeCycle(t *testing.T) {
	t.Parallel()
	root := &codec.Node{Kind: codec.SequenceNode}
	root.Content = []*codec.Node{{Kind: codec.AliasNode, Alias: root}}
	done := make(map[*codec.Node]*codec.Node)
	if _, ok := cloneActionNode(root, make(map[*codec.Node]bool), done); ok {
		t.Fatal("accepted a cyclic node graph")
	}
	if _, ok := done[root]; ok {
		t.Fatal("cached an incomplete clone")
	}
}
