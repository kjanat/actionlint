// Package execution runs prepared script analysis without discovering documents.
package execution

import (
	"context"

	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
)

// Service owns scheduling and joining every script analyzer in a prepared batch.
type Service struct{ Analyzers []Analyzer }

func (s Service) Run(ctx context.Context, batch Batch) ([]*findingmodel.Finding, error) {
	if !batch.Valid() {
		return nil, ErrInvalidBatch
	}
	tasks := make([]analyzerhost.Task[*findingmodel.Finding], len(s.Analyzers))
	for i, a := range s.Analyzers {
		tasks[i] = func(ctx context.Context) ([]*findingmodel.Finding, error) { return a.AnalyzeScripts(ctx, batch) }
	}
	return analyzerhost.RunTasks(ctx, tasks)
}
