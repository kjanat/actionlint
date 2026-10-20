package action

import (
	"encoding/json"
	"sync"
	"testing"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/yaml"
)

func TestMetadataPositionCacheConstruction(t *testing.T) {
	var metadata Metadata
	if err := json.Unmarshal([]byte(`{"name":"bundled","inputs":{"token":{"name":"token"}}}`), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.positions != nil {
		t.Fatal("JSON metadata allocated a source-position cache")
	}
	value := &ExprString{Value: "example", Line: 2, Column: 3}
	for range 2 {
		if got := ExpressionPosition(&metadata, value, 2); *got != (workflowlang.Pos{Line: 2, Col: 3}) || metadata.positions != nil {
			t.Fatal("source-less position lookup allocated a cache or lost its fallback")
		}
	}
	for _, content := range []string{"null\n", "name: source\nruns: {using: composite, steps: []}\n"} {
		parsed, err := Parse("nested/action.yml", []byte(content))
		if err != nil || parsed.positions == nil {
			t.Fatalf("source-backed metadata cache: %+v, %v", parsed, err)
		}
		if parsed.Path() != "nested/action.yml" || string(parsed.Source) != content {
			t.Fatal("parser lost source identity")
		}
	}
}

func TestMetadataPositionCache(t *testing.T) {
	const source = "name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: echo ${{ github_repository }}\n      shell: bash\n"
	var meta Metadata
	if err := yaml.Unmarshal([]byte(source), &meta); err != nil {
		t.Fatal(err)
	}
	meta.Source = []byte(source)
	value := &ExprString{Value: "echo ${{ github_repository }}", Line: 6, Column: 12}
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
			if pos := ExpressionPosition(&view, value, 10); *pos != (workflowlang.Pos{Line: 6, Col: 21}) {
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
