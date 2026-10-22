package application

import (
	"context"

	"actionlint.kjanat.dev/internal/workflow/execution"
	"actionlint.kjanat.dev/internal/workspace"
)

type gitModes struct{ workspace.IndexCache }

func (cache *gitModes) load(ctx context.Context, root string) *execution.IndexSnapshot {
	index := cache.Load(ctx, root)
	return &execution.IndexSnapshot{Modes: index.Modes, Path: index.Path, Err: index.Err}
}
