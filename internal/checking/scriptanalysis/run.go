// Package scriptanalysis runs prepared script analysis without discovering documents.
package scriptanalysis

import (
	"context"

	"actionlint.kjanat.dev/v2/internal/checking/executor"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

// Service owns scheduling and joining every script analyzer in a prepared batch.
type Service struct{ Analyzers []Analyzer }

func (s Service) Run(ctx context.Context, batch Batch) ([]*finding.Finding, error) {
	if !batch.Valid() {
		return nil, ErrInvalidBatch
	}
	tasks := make([]executor.Task[*finding.Finding], len(s.Analyzers))
	for i, a := range s.Analyzers {
		tasks[i] = func(ctx context.Context) ([]*finding.Finding, error) { return a.AnalyzeScripts(ctx, batch) }
	}
	return executor.RunTasks(ctx, tasks)
}
