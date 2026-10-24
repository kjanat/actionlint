package component

import (
	"context"
	"errors"
	"fmt"
)

type callResource interface {
	Close(context.Context) error
}

// callTeardown closes dependents before providers, even after cancellation.
// A guest close failure retires the runtime after its host imports close.
type callTeardown struct {
	plugin      *Plugin
	guestFailed bool
}

func (c *callTeardown) closeResource(ctx context.Context, name string, resource callResource, result *error) bool {
	if err := resource.Close(context.WithoutCancel(ctx)); err != nil {
		*result = errors.Join(*result, fmt.Errorf("plugin %s: close %s: %w", c.plugin.path, name, err))
		return true
	}
	return false
}

func (c *callTeardown) closeGuest(ctx context.Context, guest callResource, result *error) {
	c.guestFailed = c.closeResource(ctx, "component", guest, result)
}

func (c *callTeardown) finish(ctx context.Context, result *error) {
	p := c.plugin
	cleanup := context.WithoutCancel(ctx)
	// Guest closure does not drop outstanding handles in imported host instances.
	closeErr := p.discardImports(cleanup)
	if c.guestFailed && p.engine != nil {
		closeErr = errors.Join(closeErr, p.engine.Close(cleanup))
		p.engine = nil
	}
	if closeErr != nil {
		*result = errors.Join(*result, fmt.Errorf("plugin %s: close host imports: %w", p.path, closeErr))
	}
}
