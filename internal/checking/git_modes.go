package checking

import (
	"context"
	"sync"

	"actionlint.kjanat.dev/v2/internal/integrations/gitcommand"

	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
)

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
			index, err := gitcommand.ReadIndex(ctx, root)
			return execution.NewIndexSnapshot(index.Path, index.Modes, err)
		})
		cache.entries[root] = load
	}
	cache.mu.Unlock()
	return load()
}
