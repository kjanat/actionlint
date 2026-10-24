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

// callScope owns every instance created for a single invocation.
// Each acquisition is recorded before another allocation can fail.
type callScope struct {
	host        *host.ComponentInstance
	world       *wasi.World
	output      *lineWriter
	plugin      *Plugin
	guest       *wacogo.ComponentInstance
	stdin       *host.ComponentInstance
	annotations *host.ComponentInstance
}

func (c *callScope) invoke(ctx context.Context, iface, name string, args ...wacogo.Val) (value wacogo.Val, err error) {
	p := c.plugin
	defer func() {
		if recovered := recover(); recovered != nil {
			recoveredError, ok := recovered.(error)
			if !ok || !errors.Is(recoveredError, errMemoryBudget) {
				panic(recovered)
			}
			cleanup := context.WithoutCancel(ctx)
			closeErr := c.discardImports(cleanup)
			closeErr = errors.Join(closeErr, p.closeEngine(cleanup))
			value, err = nil, errors.Join(err, p.failure(ctx, errMemoryBudget), closeErr)
		}
	}()
	defer c.finish(ctx, &err)
	if err := c.prepareGuest(ctx); err != nil {
		return nil, p.failure(ctx, err)
	}
	exported := c.guest.ExportedInstance(iface)
	if exported == nil {
		return nil, p.failure(ctx, fmt.Errorf("instance does not export %s", iface))
	}
	fn := exported.ExportedFunc(name)
	if fn == nil {
		return nil, p.failure(ctx, fmt.Errorf("plugin does not export %s", name))
	}
	results, err := fn.Call(ctx, args...)
	if err != nil {
		return nil, p.failure(ctx, err)
	}
	if len(results) != 1 {
		return nil, p.failure(ctx, fmt.Errorf("%s returned %d values", name, len(results)))
	}
	return results[0], nil
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

func (c *callScope) prepareGuest(ctx context.Context) error {
	p := c.plugin
	if err := c.prepareImports(ctx); err != nil {
		return err
	}
	// wacogo's stdin component hands out one stream handle per host instance.
	input, err := stdin.NewInstance(ctx, p.engine, c.world.Streams, c.world.Error, c.world.Poll, nil)
	if err != nil {
		return err
	}
	c.stdin = input
	imports := append([]wacogo.InstantiateOption{wacogo.WithInstanceImport(HostInterface, c.host.Core())}, c.world.Imports()...)
	imports = append(imports, wacogo.WithInstanceImport(stdin.InterfaceName, input.Core()))
	if p.annotationsFactory != nil {
		annotations, err := p.annotationsFactory.NewInstance(ctx, sourceAnnotations{}, nil)
		if err != nil {
			return err
		}
		c.annotations = annotations
		imports = append(imports, wacogo.WithInstanceImport(SourceAnnotationsInterface, annotations.Core()))
	}
	guest, err := p.component.Instantiate(ctx, imports...)
	if err != nil {
		return fmt.Errorf("instantiate: %w", err)
	}
	c.guest = guest
	return nil
}
