package analyzerhost

import (
	"context"

	"actionlint.kjanat.dev/v2/internal/analyzerhost/executor"
)

// Task returns partial findings even when its analysis fails. It must finish
// child work before returning; cancellation is cooperative.
type Task[T any] = executor.Task[T]

// RunTasks joins independent analyses and combines outcomes in registration
// order.
func RunTasks[T any](ctx context.Context, tasks []Task[T]) ([]T, error) {
	return executor.RunTasks(ctx, tasks)
}
