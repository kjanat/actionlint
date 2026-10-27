package application

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/documentparse"
)

func seedCapturedWorkflow(t testing.TB, cache *capturedWorkflowCache, path string, content []byte) {
	t.Helper()
	snapshot, err := cache.snapshots.Document(path, content, documentparse.Workflow(path, content))
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Seed(snapshot); err != nil {
		t.Fatal(err)
	}
}
