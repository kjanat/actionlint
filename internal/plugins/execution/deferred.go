package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	corewasm "actionlint.kjanat.dev/v2/internal/plugins/execution/module"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

type deferredCoreEngine struct {
	gate        chan struct{}
	descriptor  checkinputs.ScriptEngineDescriptor
	pin         plugininstall.Pin
	acquisition plugininstall.Acquisition
	backend     *corewasm.Engine
	closed      bool
}

func newDeferredCoreEngine(pin plugininstall.Pin, acquisition plugininstall.Acquisition) (*deferredCoreEngine, error) {
	metadata, err := corewasm.ReadMetadata([]byte(pin.Metadata))
	if err != nil {
		return nil, err
	}
	descriptor := metadata.ScriptDescriptor()
	for _, operation := range metadata.Interfaces["check"] {
		if operation.Version == 3 {
			descriptor.Fixes, descriptor.ReportedSources = metadata.Capabilities.Fixes, metadata.Capabilities.ReportedSources
			descriptor.Resources = slices.Clone(metadata.Capabilities.Resources)
			break
		}
	}
	return &deferredCoreEngine{gate: make(chan struct{}, 1), descriptor: descriptor, pin: pin, acquisition: acquisition}, nil
}

func (b *deferredCoreEngine) get(ctx context.Context) (*corewasm.Engine, error) {
	select {
	case b.gate <- struct{}{}:
		defer func() { <-b.gate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.closed {
		return nil, errors.New("default core engine is closed")
	}
	if b.backend != nil {
		return b.backend, nil
	}
	releases := plugininstall.ResolvedPlugins{Plugins: []plugininstall.Pin{b.pin}}
	if err := releases.CheckStatuses(ctx, plugininstall.StatusOptions{Acquisition: b.acquisition}); err != nil {
		return nil, err
	}
	artifact, err := pluginartifact.Resolve(ctx, b.pin.Source(), b.acquisition.ArtifactOptions())
	if err != nil {
		return nil, err
	}
	if err := b.pin.Verify(artifact.Info); err != nil {
		return nil, err
	}
	engine, err := corewasm.LoadArtifact(ctx, artifact, corewasm.Options{CacheDir: b.acquisition.CompiledModuleCacheDir()})
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(b.descriptor, engine.ScriptDescriptor()) {
		return nil, errors.Join(fmt.Errorf("default engine %s runtime routing disagrees with registry metadata", b.pin.Name), engine.Close(context.WithoutCancel(ctx)))
	}
	b.backend = engine
	return engine, nil
}

func (b *deferredCoreEngine) ScriptDescriptor() checkinputs.ScriptEngineDescriptor {
	return b.descriptor.Clone()
}

func (b *deferredCoreEngine) Check(ctx context.Context, request checkinputs.ScriptRequest) ([]checkinputs.ScriptDiagnostic, error) {
	engine, err := b.get(ctx)
	if err != nil {
		return nil, err
	}
	return engine.Check(ctx, request)
}

func (b *deferredCoreEngine) ValidateConfig(ctx context.Context, config json.RawMessage) error {
	engine, err := b.get(ctx)
	if err != nil {
		return err
	}
	return engine.ValidateConfig(ctx, config)
}

func (b *deferredCoreEngine) Close(ctx context.Context) error {
	b.gate <- struct{}{}
	defer func() { <-b.gate }()
	if b.closed {
		return nil
	}
	b.closed = true
	if b.backend == nil {
		return nil
	}
	return b.backend.Close(ctx)
}
