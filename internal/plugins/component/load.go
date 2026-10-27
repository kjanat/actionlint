package component

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifact"
	hostbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/v0.2.0/host"
	invocationbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/v0.2.0/invocationcontext"
	preparedbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/v0.2.0/preparedscripts"
	annotationsbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/v0.2.0/sourceannotations"
	filesbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/v0.2.0/sourcefiles"
	"github.com/kjanat/wacogo"
	"github.com/kjanat/wazero"
	"github.com/kjanat/wazero/api"
	"github.com/kjanat/wazero/experimental"
	"golang.org/x/sync/semaphore"
)

// Load reads and validates a component and asks it to describe itself.
func Load(ctx context.Context, path string, options Options) (*Plugin, error) {
	limits, err := options.limits()
	if err != nil {
		return nil, err
	}
	artifact, err := pluginartifact.Resolve(ctx, path, options.Artifact)
	if err != nil {
		return nil, err
	}
	return loadArtifact(ctx, artifact, limits, options.Log)
}

// LoadArtifact loads a resolved snapshot without reading its source again.
func LoadArtifact(ctx context.Context, artifact *pluginartifact.Artifact, options Options) (*Plugin, error) {
	limits, err := options.limits()
	if err != nil {
		return nil, err
	}
	return loadArtifact(ctx, artifact, limits, options.Log)
}

func loadArtifact(ctx context.Context, artifact *pluginartifact.Artifact, limits Limits, logLine func(string)) (*Plugin, error) {
	path, data := artifact.Info.Source, artifact.Bytes
	kind, staticMetadata, err := pluginartifact.Sniff(data)
	if err != nil {
		return nil, err
	}
	if kind != pluginartifact.Component {
		return nil, errors.New("plugin requires a WebAssembly component")
	}
	config := wazero.NewRuntimeConfig().
		WithCoreFeatures(api.CoreFeaturesV2 | experimental.CoreFeaturesExtendedConst).
		WithCloseOnContextDone(true).
		WithMemoryLimitPages(limits.MemoryPages)
	engine := wacogo.NewEngine(ctx,
		wacogo.WithRuntimeConfig(config),
		wacogo.WithLiftLimits(wacogo.LiftLimits{MaxBytes: maxLiftBytes, MaxValues: maxLiftValues}),
	)
	p := &Plugin{
		path: path, digest: artifact.Info.SHA256,
		limits: limits, log: logLine, engine: engine,
		gate:   semaphore.NewWeighted(1),
		memory: &memoryBudget{limit: uint64(limits.MemoryPages) << 16},
	}
	if err := p.load(ctx, data, staticMetadata); err != nil {
		return nil, fmt.Errorf("plugin %s: %w", path, errors.Join(err, p.Close(context.WithoutCancel(ctx))))
	}
	return p, nil
}

func (p *Plugin) load(ctx context.Context, data, staticMetadata []byte) error {
	component, err := p.engine.LoadComponent(ctx, bytes.NewReader(data))
	if err != nil {
		return err
	}
	roles, err := extensionRoles(component.Exports())
	if err != nil {
		return err
	}
	required, err := validateImports(component.Imports())
	if err != nil {
		return err
	}
	if err := checkRoleContracts(data, roles); err != nil {
		return err
	}
	p.component = component
	p.hostFactory, err = hostbind.NewFactory(ctx, p.engine)
	if err != nil {
		return err
	}
	if required.invocations || required.preparedScripts || required.sourceAnnotations {
		p.invocationFactory, err = invocationbind.NewFactory(ctx, p.engine)
		if err != nil {
			return err
		}
	}
	if required.sourceAnnotations {
		p.annotationsFactory, err = annotationsbind.NewFactory(ctx, p.engine)
		if err != nil {
			return err
		}
	}
	if required.sourceFiles {
		p.filesFactory, err = filesbind.NewFactory(ctx, p.engine)
		if err != nil {
			return err
		}
	}
	if required.preparedScripts {
		p.preparedFactory, err = preparedbind.NewFactory(ctx, p.engine)
		if err != nil {
			return err
		}
	}
	if roles.analyzer {
		if err := p.loadAnalyzer(ctx); err != nil {
			return err
		}
	}
	if roles.toolIntegration {
		if err := p.loadToolIntegration(ctx); err != nil {
			return err
		}
	}
	if roles.runner {
		if err := p.loadRunner(ctx); err != nil {
			return err
		}
	}
	if roles.script {
		if err := p.loadScriptEngine(ctx, staticMetadata); err != nil {
			return err
		}
	}
	if roles.templateEffects {
		if err := p.loadTemplateEffects(ctx, staticMetadata); err != nil {
			return err
		}
	} else if roles.script {
		if err := verifyTemplateMetadata(staticMetadata, ""); err != nil {
			return err
		}
	}

	return nil
}
