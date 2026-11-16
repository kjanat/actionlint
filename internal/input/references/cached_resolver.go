package references

import (
	"context"
	"sync"

	"actionlint.kjanat.dev/v2/internal/integrations/acquisitionhttp/accounting"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
)

// CachedResolver shares exact resolutions, including consumer kind and
// expected repository identity, within one session. Concurrent requests for
// the same Request wait for a single call to the wrapped resolver. Successful
// resolutions are kept; failures are not. A waiter whose leader was cancelled
// resolves the request itself instead of sharing the cancellation.
type CachedResolver struct {
	resolver Resolver
	mu       sync.Mutex
	cache    map[Request]ResolutionRecord
	pending  map[Request]*pendingResolution
}

// NewCachedResolver caches resolutions of resolver for one session.
func NewCachedResolver(resolver Resolver) *CachedResolver {
	return &CachedResolver{resolver: resolver}
}

type pendingResolution struct {
	done      chan struct{}
	result    ResolutionRecord
	err       error
	abandoned bool
}

// Resolve returns the session's resolution of request, asking the wrapped
// resolver only when no earlier or concurrent call can supply it. Waiting for
// a concurrent call does not consume the reference lookup budget.
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
