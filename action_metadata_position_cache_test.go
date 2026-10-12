package actionlint

import (
	"sync"
	"testing"

	"go.yaml.in/yaml/v4"
)

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
			if pos := metadataExpressionPosition(&view, value, 10); *pos != (Pos{Line: 6, Col: 21}) {
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
