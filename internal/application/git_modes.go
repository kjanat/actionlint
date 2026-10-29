package application

import (
	"context"

	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

type gitModes struct{ workspace.IndexCache }

func (cache *gitModes) load(ctx context.Context, root string) *execution.IndexSnapshot {
	index := cache.Load(ctx, root)
	return execution.NewIndexSnapshot(index.Path, index.Modes, index.Err)
}
