package component

import (
	"context"
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/guestlimits"
	"actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint-resolver/repositoryobjects"
	"actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint-script/environments"
	"actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint-script/shellscriptinputs"
	"actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint/host"
	"actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint/invocationcontext"
	"actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint/sourceannotations"
	"actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint/sourcefiles"
	"github.com/kjanat/wacogo"
	"golang.org/x/sync/semaphore"
)

func LoadComponentPlugin(ctx context.Context, path string, options Options) (*Plugin, error) {
	limits, err := options.limits()
	if err != nil {
		return nil, err
	}
	artifact, err := artifacts.Resolve(ctx, path, options.Artifact)
	if err != nil {
		return nil, err
	}
	return loadArtifact(ctx, artifact, limits, options)
}

func LoadArtifact(ctx context.Context, artifact *artifacts.Artifact, options Options) (*Plugin, error) {
	limits, err := options.limits()
	if err != nil {
		return nil, err
	}
	return loadArtifact(ctx, artifact, limits, options)
}

func loadArtifact(ctx context.Context, artifact *artifacts.Artifact, limits guestlimits.Limits, options Options) (*Plugin, error) {
	path := artifact.Info.Source
	engine := sharedComponentCompilation.newEngine(ctx, limits)
	p := &Plugin{
		path: path, digest: artifact.Info.SHA256,
		limits: limits, log: options.Log, engine: engine, reservedRules: options.ReservedRules,
		gate:   semaphore.NewWeighted(1),
		memory: &memoryBudget{limit: uint64(limits.MemoryPages) << 16},
	}
	admitted, component, err := checkComponent(ctx, engine, artifact.Bytes)
	if err == nil {
		err = p.load(ctx, component, admitted)
	}
	if err != nil {
		return nil, fmt.Errorf("plugin %s: %w", path, errors.Join(err, p.Close(context.WithoutCancel(ctx))))
	}
	return p, nil
}

func (p *Plugin) load(ctx context.Context, component *wacogo.Component, admitted admission) error {
	var err error
	roles, required := admitted.roles, admitted.required
	p.roles = roles.names()
	p.component = component
	p.referenceResolver = roles.referenceResolver
	if required.repositoryObjects {
		p.repositoryFactory, err = repositoryobjects.NewFactory(ctx, p.engine)
		if err != nil {
			return err
		}
	}
	if required.environments {
		p.environmentFactory, err = environments.NewFactory(ctx, p.engine)
		if err != nil {
			return err
		}
	}
	p.hostFactory, err = host.NewFactory(ctx, p.engine)
	if err != nil {
		return err
	}
	if required.invocations || required.sourceAnnotations {
		p.invocationFactory, err = invocationcontext.NewFactory(ctx, p.engine)
		if err != nil {
			return err
		}
	}
	if required.sourceAnnotations {
		p.annotationsFactory, err = sourceannotations.NewFactory(ctx, p.engine)
		if err != nil {
			return err
		}
	}
	if required.sourceFiles {
		p.filesFactory, err = sourcefiles.NewFactory(ctx, p.engine)
		if err != nil {
			return err
		}
	}
	if required.shellScriptInputs {
		p.shellInputFactory, err = shellscriptinputs.NewFactory(ctx, p.engine)
		if err != nil {
			return err
		}
	}
	if roles.analyzer {
		if err := p.loadAnalyzer(ctx); err != nil {
			return err
		}
		p.analyzerDescriptor.InvocationDefinitions = admitted.invocations
	}
	if roles.toolIntegration {
		if err := p.loadToolIntegration(ctx); err != nil {
			return err
		}
	}
	if roles.runner != nil {
		p.runner = roles.runner
		if err := p.loadRunner(ctx); err != nil {
			return err
		}
	}
	if roles.script != nil {
		p.script = roles.script
		if err := p.loadScriptEngine(ctx, admitted.script); err != nil {
			return err
		}
	}
	if roles.templateEffects != nil {
		if err := p.loadTemplateEffects(ctx); err != nil {
			return err
		}
	}
	return nil
}
