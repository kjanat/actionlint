package application

import (
	"context"
	"sync"

	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

// gitModes reads each repository's Git index once per analysis. Every rule
// gets the same snapshot for a root, so the directory and case-folding indexes
// it builds on first use are shared too.
type gitModes struct {
	mu      sync.Mutex
	entries map[string]func() *execution.IndexSnapshot
}

func (cache *gitModes) load(ctx context.Context, root string) *execution.IndexSnapshot {
	cache.mu.Lock()
	if cache.entries == nil {
		cache.entries = make(map[string]func() *execution.IndexSnapshot)
	}
	load := cache.entries[root]
	if load == nil {
		load = sync.OnceValue(func() *execution.IndexSnapshot {
			index, err := workspace.ReadIndex(ctx, root)
			return execution.NewIndexSnapshot(index.Path, index.Modes, err)
		})
		cache.entries[root] = load
	}
	cache.mu.Unlock()
	return load()
}
