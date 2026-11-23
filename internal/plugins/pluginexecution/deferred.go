package pluginexecution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"time"

	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/component"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/module"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
)

const deferredRetryDelay = 5 * time.Second

type deferredBackend interface {
	scriptengine.DescribedBackend
	scriptengine.ConfigValidator
	Close(context.Context) error
}

type deferredScriptEngine struct {
	gate        chan struct{}
	descriptor  scriptengine.Descriptor
	pin         pluginregistry.Pin
	acquisition dependencycache.Acquisition
	backend     deferredBackend
	failure     error
	retryAfter  time.Time
	reserved    rule.Names
	closed      bool
}

func newDeferredScriptEngine(pin pluginregistry.Pin, acquisition dependencycache.Acquisition, reserved rule.Names) (*deferredScriptEngine, error) {
	var descriptor scriptengine.Descriptor
	if pin.Kind == artifacts.Component {
		var err error
		descriptor, err = component.ReadScriptMetadata([]byte(pin.Metadata), reserved)
		if err != nil {
			return nil, err
		}
	} else {
		metadata, err := module.ReadMetadata([]byte(pin.Metadata))
		if err != nil {
			return nil, err
		}
		descriptor = metadata.ScriptDescriptor()
		for _, operation := range metadata.Interfaces["check"] {
			if operation.Version == 3 {
				descriptor.Fixes, descriptor.ReportedSources = metadata.Capabilities.Fixes, metadata.Capabilities.ReportedSources
				descriptor.Resources = slices.Clone(metadata.Capabilities.Resources)
				break
			}
		}
	}
	return &deferredScriptEngine{gate: make(chan struct{}, 1), descriptor: descriptor, pin: pin, acquisition: acquisition, reserved: reserved}, nil
}

func (b *deferredScriptEngine) get(ctx context.Context) (backend deferredBackend, err error) {
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
		return nil, errors.New("default script engine is closed")
	}
	if b.backend != nil || b.failure != nil {
		return b.backend, b.failure
	}
	defer func() {
		if err != nil && ctx.Err() == nil {
			b.failure = err
			b.retryAfter = time.Now().Add(deferredRetryDelay)
		}
	}()
	releases := pluginregistry.ResolvedPlugins{Plugins: []pluginregistry.Pin{b.pin}}
	if err := releases.CheckStatuses(ctx, pluginregistry.StatusOptions{Acquisition: b.acquisition}); err != nil {
		return nil, err
	}
	artifact, err := artifacts.Resolve(ctx, b.pin.Source(), pluginacquisition.ArtifactOptions(b.acquisition))
	if err != nil {
		return nil, err
	}
	if err := b.pin.Verify(artifact.Info); err != nil {
		return nil, err
	}
	engine, err := b.load(ctx, artifact)
	if err != nil {
		return nil, err
	}
	if !sameScriptDescriptor(b.descriptor, engine.ScriptDescriptor()) {
		return nil, errors.Join(fmt.Errorf("default engine %s runtime routing disagrees with registry metadata", b.pin.Name), engine.Close(context.WithoutCancel(ctx)))
	}
	b.backend = engine
	return engine, nil
}

func (b *deferredScriptEngine) beginAnalysis(ctx context.Context, now time.Time) error {
	select {
	case b.gate <- struct{}{}:
		defer func() { <-b.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.failure != nil && !now.Before(b.retryAfter) {
		b.failure = nil
		b.retryAfter = time.Time{}
	}
	return nil
}

func (b *deferredScriptEngine) load(ctx context.Context, artifact *artifacts.Artifact) (deferredBackend, error) {
	if b.pin.Kind == artifacts.Core {
		return module.LoadArtifact(ctx, artifact, module.Options{CacheDir: pluginacquisition.CompiledModuleCacheDir(b.acquisition)})
	}
	engine, err := component.LoadArtifact(ctx, artifact, component.Options{ReservedRules: b.reserved})
	if err != nil {
		return nil, err
	}
	if !engine.HasScriptEngine() || engine.HasAnalyzer() || engine.HasRunnerProvider() || engine.HasReferenceResolver() {
		return nil, errors.Join(errors.New("default script component exports roles not declared by its script-only metadata; configure the plugin explicitly to use all roles"), engine.Close(context.WithoutCancel(ctx)))
	}
	return engine, nil
}

func sameScriptDescriptor(left, right scriptengine.Descriptor) bool {
	leftSchema, rightSchema := left.Rule.OptionsSchema, right.Rule.OptionsSchema
	left.Rule.OptionsSchema, right.Rule.OptionsSchema = nil, nil
	if len(left.Rule.Examples) == 0 {
		left.Rule.Examples = nil
	}
	if len(right.Rule.Examples) == 0 {
		right.Rule.Examples = nil
	}
	return reflect.DeepEqual(left, right) && (len(leftSchema) == 0 && len(rightSchema) == 0 || artifacts.SameMetadata(leftSchema, rightSchema))
}

func (b *deferredScriptEngine) ScriptDescriptor() scriptengine.Descriptor {
	return b.descriptor.Clone()
}

func (b *deferredScriptEngine) Check(ctx context.Context, request scriptengine.Request) ([]scriptengine.Diagnostic, error) {
	engine, err := b.get(ctx)
	if err != nil {
		return nil, err
	}
	return engine.Check(ctx, request)
}

func (b *deferredScriptEngine) ValidateConfig(ctx context.Context, config json.RawMessage) error {
	engine, err := b.get(ctx)
	if err != nil {
		return err
	}
	return engine.ValidateConfig(ctx, config)
}

func (b *deferredScriptEngine) Close(ctx context.Context) error {
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
