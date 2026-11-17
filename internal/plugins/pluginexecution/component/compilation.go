package component

import (
	"bytes"
	"context"

	"github.com/kjanat/wacogo"
	"github.com/kjanat/wazero"
	"github.com/kjanat/wazero/api"
	"github.com/kjanat/wazero/experimental"
	"golang.org/x/sync/semaphore"
)

// Compiled code lives for the process. Each plugin owns a separate runtime and
// guest memory, so closing one plugin leaves compiled code available to later loads.
var sharedComponentCompilation = newComponentCompilation()

type componentCompilation struct {
	cache wazero.CompilationCache
	gate  *semaphore.Weighted
}

func newComponentCompilation() *componentCompilation {
	return &componentCompilation{cache: wazero.NewCompilationCache(), gate: semaphore.NewWeighted(1)}
}

func (c *componentCompilation) newEngine(ctx context.Context, limits Limits) *wacogo.Engine {
	config := wazero.NewRuntimeConfig().
		WithCoreFeatures(api.CoreFeaturesV2 | experimental.CoreFeaturesExtendedConst).
		WithCloseOnContextDone(true).
		WithMemoryLimitPages(limits.MemoryPages).
		WithCompilationCache(c.cache)
	return wacogo.NewEngine(ctx,
		wacogo.WithRuntimeConfig(config),
		wacogo.WithLiftLimits(wacogo.LiftLimits{MaxBytes: maxLiftBytes, MaxValues: maxLiftValues}),
	)
}

func (c *componentCompilation) load(ctx context.Context, engine *wacogo.Engine, data []byte) (*wacogo.Component, error) {
	// Wazero's cache allows concurrent misses to compile the same module. Finish
	// each component's compilation before another load checks the shared cache.
	if err := c.gate.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer c.gate.Release(1)
	return engine.LoadComponent(ctx, bytes.NewReader(data))
}
