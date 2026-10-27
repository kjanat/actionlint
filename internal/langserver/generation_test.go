package langserver

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
)

func TestBufferedStaleGenerationRejectsErrorAndCapturedText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workflow.yml")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	results := make(chan analysisResult, 1)
	// The callback ignores cancellation and completes while generation 1 is
	// current. Its result is already buffered before generation 2 is scheduled;
	// cancellation cannot choose the producer's discard branch in this test.
	analyzeGeneration(ctx, func(context.Context, map[string]Document) (Analysis, error) {
		return Analysis{Diagnostics: []diagnostic.Diagnostic{{Path: path, Message: "stale", Start: diagnostic.Position{Line: 1, Column: 1}}}, Sources: map[string]Source{path: testSource(t, path, "stale captured text")}}, errors.New("source-revision-conflict: stale")
	}, nil, 1, results)
	if len(results) != 1 {
		t.Fatal("old generation result was not held in the queue")
	}
	cancel()
	queued := <-results
	analysis, err, current := currentAnalysis(queued, 2, false)
	if current || err != nil || len(analysis.Diagnostics) != 0 || len(analysis.Sources) != 0 {
		t.Fatalf("generation guard admitted stale error or text: analysis=%+v error=%v current=%v", analysis, err, current)
	}
}
