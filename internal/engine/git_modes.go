package engine

import (
	"context"
	"sync"

	"actionlint.kjanat.dev/internal/workflow/execution"
	"actionlint.kjanat.dev/internal/workspace"
)

// Each analysis gets fresh Git index snapshots. Reading Git remains host work;
// path traversal and case inference belong to the immutable execution snapshot.
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
			return &execution.IndexSnapshot{Modes: index.Modes, Path: index.Path, Err: err}
		})
		cache.entries[root] = load
	}
	cache.mu.Unlock()
	return load()
}
