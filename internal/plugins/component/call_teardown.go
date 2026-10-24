package component

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/kjanat/wacogo"
	"github.com/kjanat/wacogo/host"
	"github.com/kjanat/wacogo/wasi"
	"github.com/kjanat/wacogo/wasi/cli/stdin"
	"github.com/kjanat/wacogo/wasi/filesystem/preopens"
)

type callResource interface {
	Close(context.Context) error
}

// callScope owns the host imports and output of one invocation.
// It closes dependents before providers, even after cancellation.
// A guest close failure retires the runtime after its host imports close.
type callScope struct {
	host        *host.ComponentInstance
	world       *wasi.World
	output      *lineWriter
	plugin      *Plugin
	guestFailed bool
}

func (c *callScope) closeResource(ctx context.Context, name string, resource callResource, result *error) bool {
	if err := resource.Close(context.WithoutCancel(ctx)); err != nil {
		*result = errors.Join(*result, fmt.Errorf("plugin %s: close %s: %w", c.plugin.path, name, err))
		return true
	}
	return false
}

func (c *callScope) closeGuest(ctx context.Context, guest callResource, result *error) {
	c.guestFailed = c.closeResource(ctx, "component", guest, result)
}

func (c *callScope) finish(ctx context.Context, result *error) {
	p := c.plugin
	cleanup := context.WithoutCancel(ctx)
	// Guest closure does not drop outstanding handles in imported host instances.
	closeErr := c.discardImports(cleanup)
	if c.guestFailed && p.engine != nil {
		closeErr = errors.Join(closeErr, p.engine.Close(cleanup))
		p.engine = nil
	}
	if closeErr != nil {
		*result = errors.Join(*result, fmt.Errorf("plugin %s: close host imports: %w", p.path, closeErr))
	}
}

func (c *callScope) prepareImports(ctx context.Context) error {
	p := c.plugin
	output := &lineWriter{log: p.log}
	hostInstance, err := p.factory.NewInstance(ctx, hostImplementation{output: output}, nil)
	if err != nil {
		return err
	}
	// The call owns each acquired import before the next allocation can fail.
	c.host, c.output = hostInstance, output
	c.world, err = wasi.NewWorld(ctx, p.engine, &wasi.Config{
		Args: []string{filepath.Base(p.path)}, Stdout: output, Stderr: output,
		Preopens: func(deps preopens.Deps) preopens.Preopens { return scopedPreopens{deps} },
	})
	return err
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

// invoke acquires imports before the guest. Defers release the guest, annotations,
// stdin, output and host imports in that order, before runtime recovery.
func (c *callScope) invoke(ctx context.Context, iface, name string, args ...wacogo.Val) (results []wacogo.Val, err error) {
	p := c.plugin
	defer func() {
		if recovered := recover(); recovered != nil {
			recoveredError, ok := recovered.(error)
			if !ok || !errors.Is(recoveredError, errMemoryBudget) {
				panic(recovered)
			}
			cleanup := context.WithoutCancel(ctx)
			closeErr := c.discardImports(cleanup)
			if p.engine != nil {
				closeErr = errors.Join(closeErr, p.engine.Close(cleanup))
			}
			results, err = nil, errors.Join(err, p.failure(ctx, errMemoryBudget), closeErr)
			p.engine = nil
		}
	}()
	defer c.finish(ctx, &err)
	if err := c.prepareImports(ctx); err != nil {
		return nil, p.failure(ctx, err)
	}
	output := c.output
	defer func() {
		output.flush()
		if outputErr := output.err; outputErr != nil {
			err = errors.Join(err, p.failure(ctx, outputErr))
		}
	}()
	// wacogo's stdin component hands out one stream handle per host instance.
	input, err := stdin.NewInstance(ctx, p.engine, c.world.Streams, c.world.Error, c.world.Poll, nil)
	if err != nil {
		return nil, p.failure(ctx, err)
	}
	defer c.closeResource(ctx, "stdin", input, &err)
	imports := append([]wacogo.InstantiateOption{wacogo.WithInstanceImport(HostInterface, c.host.Core())}, c.world.Imports()...)
	imports = append(imports, wacogo.WithInstanceImport(stdin.InterfaceName, input.Core()))
	if p.annotationsFactory != nil {
		annotations, err := p.annotationsFactory.NewInstance(ctx, sourceAnnotations{}, nil)
		if err != nil {
			return nil, p.failure(ctx, err)
		}
		defer c.closeResource(ctx, "source annotations", annotations, &err)
		imports = append(imports, wacogo.WithInstanceImport(SourceAnnotationsInterface, annotations.Core()))
	}
	guest, err := p.component.Instantiate(ctx, imports...)
	if err != nil {
		return nil, p.failure(ctx, fmt.Errorf("instantiate: %w", err))
	}
	defer c.closeGuest(ctx, guest, &err)
	exported := guest.ExportedInstance(iface)
	if exported == nil {
		return nil, p.failure(ctx, fmt.Errorf("instance does not export %s", iface))
	}
	fn := exported.ExportedFunc(name)
	if fn == nil {
		return nil, p.failure(ctx, fmt.Errorf("plugin does not export %s", name))
	}
	results, err = fn.Call(ctx, args...)
	if err != nil {
		return nil, p.failure(ctx, err)
	}
	if len(results) != 1 {
		return nil, p.failure(ctx, fmt.Errorf("%s returned %d values", name, len(results)))
	}
	return results, nil
}
