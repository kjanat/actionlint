package checking

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/parsing"
)

func seedCapturedWorkflow(tb testing.TB, cache *capturedWorkflowCache, path string, content []byte) {
	tb.Helper()
	snapshot, err := cache.snapshots.Document(path, content, parsing.Workflow(content))
	if err != nil {
		tb.Fatal(err)
	}
	if err := cache.Seed(snapshot); err != nil {
		tb.Fatal(err)
	}
}
