package lsp

import "context"

func analyzeGeneration(ctx context.Context, analyze Analyze, documents map[string]Document, generation int, results chan<- analysisResult) {
	analysis, err := analyze(ctx, documents)
	select {
	case results <- analysisResult{generation, analysis, err}:
	case <-ctx.Done():
	}
}

func currentAnalysis(result analysisResult, generation int, shutdown bool) (Analysis, error, bool) {
	if result.generation != generation || shutdown {
		return Analysis{}, nil, false
	}
	return result.analysis, result.err, true
}
