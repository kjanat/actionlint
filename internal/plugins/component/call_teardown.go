package component

import (
	"context"
	"errors"
	"fmt"
)

type callResource interface {
	Close(context.Context) error
}

// finish releases dependents before providers: guest, annotations, stdin,
// output, then host imports. Defers keep later cleanup running after a panic.
// Detach resources first so repeated cleanup cannot close them twice.
func (c *callScope) finish(ctx context.Context, result *error) {
	guest, annotations, input, output := c.guest, c.annotations, c.stdin, c.output
	c.guest, c.annotations, c.stdin, c.output = nil, nil, nil, nil
	cleanup := context.WithoutCancel(ctx)
	guestFailed := false
	defer func() {
		if guestFailed {
			if err := c.plugin.closeEngine(cleanup); err != nil {
				*result = errors.Join(*result, fmt.Errorf("plugin %s: close runtime: %w", c.plugin.path, err))
			}
		}
	}()
	defer func() {
		if err := c.discardImports(cleanup); err != nil {
			*result = errors.Join(*result, fmt.Errorf("plugin %s: close host imports: %w", c.plugin.path, err))
		}
	}()
	if output != nil {
		defer func() {
			output.flush()
			if output.err != nil {
				*result = errors.Join(*result, c.plugin.failure(ctx, output.err))
			}
		}()
	}
	if input != nil {
		defer c.closeResource(cleanup, "stdin", input, result)
	}
	if annotations != nil {
		defer c.closeResource(cleanup, "source annotations", annotations, result)
	}
	if guest != nil {
		guestFailed = c.closeResource(cleanup, "component", guest, result)
	}
}

func (c *callScope) closeResource(ctx context.Context, name string, resource callResource, result *error) bool {
	if err := resource.Close(context.WithoutCancel(ctx)); err != nil {
		*result = errors.Join(*result, fmt.Errorf("plugin %s: close %s: %w", c.plugin.path, name, err))
		return true
	}
	return false
}

func (c *callScope) discardImports(ctx context.Context) error {
	world, hostInstance := c.world, c.host
	c.world, c.host, c.output = nil, nil, nil
	var errs []error
	if world != nil {
		errs = append(errs, world.Close(ctx))
	}
	if hostInstance != nil {
		errs = append(errs, hostInstance.Close(ctx))
	}
	return errors.Join(errs...)
}
