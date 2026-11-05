package checking

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/parsing"
)

func seedCapturedWorkflow(t testing.TB, cache *capturedWorkflowCache, path string, content []byte) {
	t.Helper()
	snapshot, err := cache.snapshots.Document(path, content, parsing.Workflow(content))
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Seed(snapshot); err != nil {
		t.Fatal(err)
	}
}
