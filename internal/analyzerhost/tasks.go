package analyzerhost

import (
	"context"
	"errors"

	"golang.org/x/sync/errgroup"
)

// Task returns partial findings even when its analysis fails. It must finish
// child work before returning; cancellation is cooperative.
type Task[T any] func(context.Context) ([]T, error)

// RunTasks joins independent analyses and combines outcomes in registration
// order. One failure does not cancel peers or discard completed findings.
func RunTasks[T any](ctx context.Context, tasks []Task[T]) ([]T, error) {
	type outcome struct {
		findings []T
		err      error
	}
	outcomes := make([]outcome, len(tasks))
	var group errgroup.Group
	group.SetLimit(8)
	for i, task := range tasks {
		group.Go(func() error {
			if err := ctx.Err(); err != nil {
				outcomes[i].err = err
				return err
			}
			outcomes[i].findings, outcomes[i].err = task(ctx)
			return nil
		})
	}
	_ = group.Wait()
	var findings []T
	failures := make([]error, 0, len(outcomes))
	for _, outcome := range outcomes {
		findings = append(findings, outcome.findings...)
		failures = append(failures, outcome.err)
	}
	return findings, errors.Join(failures...)
}
