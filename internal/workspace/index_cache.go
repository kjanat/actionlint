package workspace

import (
	"context"
	"sync"
)

// IndexSnapshot is the result of reading a repository's Git index.
type IndexSnapshot struct {
	GitIndex
	Err error
}

// IndexCache shares immutable index observations within one analysis session.
type IndexCache struct {
	mu      sync.Mutex
	entries map[string]func() *IndexSnapshot
}

func (cache *IndexCache) Load(ctx context.Context, root string) *IndexSnapshot {
	cache.mu.Lock()
	if cache.entries == nil {
		cache.entries = make(map[string]func() *IndexSnapshot)
	}
	load := cache.entries[root]
	if load == nil {
		load = sync.OnceValue(func() *IndexSnapshot {
			index, err := ReadIndex(ctx, root)
			return &IndexSnapshot{GitIndex: index, Err: err}
		})
		cache.entries[root] = load
	}
	cache.mu.Unlock()
	return load()
}
