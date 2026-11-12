package lsp

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestBufferedStaleGenerationRejectsErrorAndCapturedText(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "workflow.yml")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	completedAnalyses := make(chan analysisResult, 1)
	// The callback ignores cancellation and completes while generation 1 is
	// current. Its result is already buffered before generation 2 is scheduled;
	// cancellation cannot choose the producer's discard branch in this test.
	analyzeGeneration(ctx, func(context.Context, map[string]Document) (Analysis, error) {
		return Analysis{Diagnostics: []results.Diagnostic{{Path: path, Message: "stale", Start: sourcepos.Position{Line: 1, Column: 1}}}, Sources: map[string]Source{path: testSource(t, path, "stale captured text")}}, errors.New("source-revision-conflict: stale")
	}, nil, 1, completedAnalyses)
	if len(completedAnalyses) != 1 {
		t.Fatal("old generation result was not held in the queue")
	}
	cancel()
	queued := <-completedAnalyses
	analysis, err, current := currentAnalysis(queued, 2, false)
	if current || err != nil || len(analysis.Diagnostics) != 0 || len(analysis.Sources) != 0 {
		t.Fatalf("generation guard admitted stale error or text: analysis=%+v error=%v current=%v", analysis, err, current)
	}
}
