package source

import (
	"bytes"
	"context"
	"sync"
)

type readCacheKey struct{}

type cachedRead struct {
	ready   chan struct{}
	content []byte
	err     error
}

type readCache struct {
	mu      sync.Mutex
	entries map[string]*cachedRead
}

// WithReadCache shares successful config reads across the phases of one command.
// ReadConfig uses this snapshot; ReadFile reads current contents.
func WithReadCache(ctx context.Context) context.Context {
	if _, ok := ctx.Value(readCacheKey{}).(*readCache); ok {
		return ctx
	}
	return context.WithValue(ctx, readCacheKey{}, &readCache{entries: make(map[string]*cachedRead)})
}

func (c *readCache) read(ctx context.Context, name string, load func() ([]byte, error)) ([]byte, error) {
	c.mu.Lock()
	entry, exists := c.entries[name]
	if !exists {
		entry = &cachedRead{ready: make(chan struct{})}
		c.entries[name] = entry
	}
	c.mu.Unlock()
	if exists {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-entry.ready:
			return bytes.Clone(entry.content), entry.err
		}
	}
	content, err := load()
	c.mu.Lock()
	entry.content, entry.err = bytes.Clone(content), err
	if err != nil {
		delete(c.entries, name)
	}
	close(entry.ready)
	c.mu.Unlock()
	return bytes.Clone(content), err
}
