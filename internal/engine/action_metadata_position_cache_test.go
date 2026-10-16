package engine

import (
	"encoding/json"
	"sync"
	"testing"

	"actionlint.kjanat.dev/internal/workspace"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/yaml"
)

func TestMetadataPositionCacheConstruction(t *testing.T) {
	value := &ActionExprString{Value: "example", Line: 2, Column: 3}
	checkSourceLess := func(meta *ActionMetadata) {
		t.Helper()
		if meta.positions != nil {
			t.Fatal("source-less metadata has a position cache")
		}
		for range 2 {
			if got := metadataExpressionPosition(meta, value, 2); *got != (workflowlang.Pos{Line: 2, Col: 3}) || meta.positions != nil {
				t.Fatal("source-less metadata did not retain its fallback position")
			}
		}
	}
	for spec, meta := range PopularActions {
		if meta == nil {
			t.Fatalf("generated action %s has no metadata", spec)
		}
		checkSourceLess(meta)
	}
	var decoded struct {
		Metadata *ActionMetadata `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(`{"metadata":{"name":"test","inputs":{}}}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Metadata == nil {
		t.Fatal("JSONL-shaped metadata was not decoded")
	}
	checkSourceLess(decoded.Metadata)
	if err := json.Unmarshal([]byte(`{"inputs":{"token":{"name":"token"}}}`), decoded.Metadata); err != nil {
		t.Fatal(err)
	}
	if decoded.Metadata.Name != "test" || decoded.Metadata.Inputs["token"] == nil {
		t.Fatal("JSON update lost existing fields or did not decode new fields")
	}
	checkSourceLess(decoded.Metadata)
	root := t.TempDir()
	writeShellcheckFixture(t, root, "source/action.yml", "name: source\ndescription: source\nruns: {using: composite, steps: []}\n")
	base := NewLocalActionsCache(workspace.Identity(root), nil)
	meta, _, err := base.FindMetadata("./source")
	if err != nil || meta == nil || meta.positions == nil {
		t.Fatalf("local metadata cache: %+v, %v", meta, err)
	}
	view := &LocalActionsCache{base: base, sourceMappings: LocalActionSources{"./generated": "./source"}}
	mapped, _, err := view.FindMetadata("./generated")
	if err != nil || mapped == nil || mapped.positions != meta.positions {
		t.Fatalf("mapped metadata lost shared cache: %+v, %v", mapped, err)
	}
	writeShellcheckFixture(t, root, "empty/action.yml", "null\n")
	empty, _, err := base.FindMetadata("./empty")
	if err != nil || empty == nil || empty.positions == nil {
		t.Fatalf("null YAML bypassed cache initialization: %+v, %v", empty, err)
	}
	for _, content := range []string{"null\n", "name: standalone\nruns: {using: composite, steps: []}\n"} {
		outline, err := ParseActionOutline("action.yml", []byte(content))
		if (err != nil) != (content == "null\n") || outline.Path != "action.yml" {
			t.Fatalf("standalone construction: %+v, %v", outline, err)
		}
	}
	checkSourceLess(&ActionMetadata{})
}

func TestMetadataPositionCache(t *testing.T) {
	const source = "name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: echo ${{ github_repository }}\n      shell: bash\n"
	var meta ActionMetadata
	if err := yaml.Unmarshal([]byte(source), &meta); err != nil {
		t.Fatal(err)
	}
	meta.src = []byte(source)
	value := &ActionExprString{Value: "echo ${{ github_repository }}", Line: 6, Column: 12}
	if meta.positions == nil {
		t.Fatal("parsed metadata has no position cache")
	}
	first := meta.positions.source(&meta, value)
	if first == nil {
		t.Fatal("plain scalar mapping missing")
	}
	view := meta
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			if got := view.positions.source(&view, value); got != first {
				t.Errorf("copied metadata did not reuse scalar mapping")
			}
			if pos := metadataExpressionPosition(&view, value, 10); *pos != (workflowlang.Pos{Line: 6, Col: 21}) {
				t.Errorf("position = %+v", pos)
			}
		})
	}
	workers.Wait()
	if len(meta.positions.sources) != 1 {
		t.Fatalf("expected one cached scalar, got %d", len(meta.positions.sources))
	}
	mismatch := *value
	mismatch.Value = "different value"
	if got := meta.positions.source(&meta, &mismatch); got != nil {
		t.Fatal("mapping reused for a different decoded scalar")
	}
	oldCache := meta.positions
	if err := yaml.Unmarshal([]byte(source), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.positions == oldCache || meta.positions.scalars != nil {
		t.Fatal("reparsing metadata retained stale source mappings")
	}
}
