package references

import (
	"context"
	"sync"

	"actionlint.kjanat.dev/v2/internal/integrations/acquisitionhttp/accounting"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
)

// CachedResolver caches successful resolutions for one session. A waiter whose
// leader was cancelled resolves the request itself.
type CachedResolver struct {
	resolver Resolver
	mu       sync.Mutex
	cache    map[Request]ResolutionRecord
	pending  map[Request]*pendingResolution
}

func NewCachedResolver(resolver Resolver) *CachedResolver {
	return &CachedResolver{resolver: resolver}
}

type pendingResolution struct {
	done      chan struct{}
	result    ResolutionRecord
	err       error
	abandoned bool
}

// Resolve does not consume the reference lookup budget while it waits for a
// concurrent call.
func (c *CachedResolver) Resolve(ctx context.Context, request Request) (ResolutionRecord, error) {
	for {
		if err := ctx.Err(); err != nil {
			return ResolutionRecord{}, err
		}
		c.mu.Lock()
		if result, ok := c.cache[request]; ok {
			c.mu.Unlock()
			accounting.RecordResolverCall(ctx, nil, true)
			return result, nil
		}
		pending, ok := c.pending[request]
		if !ok {
			break
		}
		c.mu.Unlock()
		if err := network.WaitOutsideLookupBudget(ctx, func() error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-pending.done:
				return ctx.Err()
			}
		}); err != nil {
			return ResolutionRecord{}, err
		}
		if !pending.abandoned {
			accounting.RecordResolverCall(ctx, nil, true)
			return pending.result, pending.err
		}
	}
	if c.pending == nil {
		c.pending = make(map[Request]*pendingResolution)
	}
	pending := &pendingResolution{done: make(chan struct{})}
	c.pending[request] = pending
	c.mu.Unlock()
	result, err := c.resolver.Resolve(ctx, request)
	c.mu.Lock()
	pending.result, pending.err, pending.abandoned = result, err, err != nil && ctx.Err() != nil
	if err == nil {
		if c.cache == nil {
			c.cache = make(map[Request]ResolutionRecord)
		}
		c.cache[request] = result
	}
	delete(c.pending, request)
	close(pending.done)
	c.mu.Unlock()
	return result, err
}
