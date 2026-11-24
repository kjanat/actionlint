// Package component loads typed WebAssembly analyzer, provider and script-engine
// components.
package component

import (
	"context"
	"errors"

	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
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

const AnalyzerInterface = canonicalAnalyzerInterface

const HostInterface = host.InterfaceName

const SourceAnnotationsInterface = sourceannotations.InterfaceName

// Plugin serializes calls, and each analysis runs in a fresh instance.
type Plugin struct {
	reservedRules rule.Names
	// A non-nil descriptor identifies a validated, available role.
	analyzerDescriptor *checkinputs.Descriptor
	runnerDescriptor   *runnerapi.Descriptor
	scriptDescriptor   *scriptengine.Descriptor
	toolIntegration    *checkinputs.ToolIntegration
	referenceResolver  bool
	roles              Roles
	runner             runnerProtocol
	// With the template-effects role, the paired template-effects check
	// replaces the script engine's check.
	script scriptEngineProtocol

	digest string
	path   string
	limits guestlimits.Limits
	log    func(string)

	engine             *wacogo.Engine
	component          *wacogo.Component
	hostFactory        *host.Factory
	annotationsFactory *sourceannotations.Factory
	filesFactory       *sourcefiles.Factory
	shellInputFactory  *shellscriptinputs.Factory
	invocationFactory  *invocationcontext.Factory
	environmentFactory *environments.Factory
	repositoryFactory  *repositoryobjects.Factory

	gate   *semaphore.Weighted
	memory *memoryBudget
}

func (p *Plugin) ArtifactDigest() string { return p.digest }

func (p *Plugin) Roles() Roles { return p.roles }

func (p *Plugin) Path() string { return p.path }

func (p *Plugin) Limits() guestlimits.Limits { return p.limits }

func (p *Plugin) Close(ctx context.Context) error {
	if err := p.gate.Acquire(ctx, 1); err != nil {
		return err
	}
	defer p.gate.Release(1)
	var errs []error
	if p.repositoryFactory != nil {
		errs = append(errs, p.repositoryFactory.Close(ctx))
		p.repositoryFactory = nil
	}
	if p.environmentFactory != nil {
		errs = append(errs, p.environmentFactory.Close(ctx))
		p.environmentFactory = nil
	}
	if p.shellInputFactory != nil {
		errs = append(errs, p.shellInputFactory.Close(ctx))
		p.shellInputFactory = nil
	}
	if p.filesFactory != nil {
		errs = append(errs, p.filesFactory.Close(ctx))
		p.filesFactory = nil
	}
	if p.hostFactory != nil {
		errs = append(errs, p.hostFactory.Close(ctx))
		p.hostFactory = nil
	}
	if p.annotationsFactory != nil {
		errs = append(errs, p.annotationsFactory.Close(ctx))
		p.annotationsFactory = nil
	}
	if p.invocationFactory != nil {
		errs = append(errs, p.invocationFactory.Close(ctx))
		p.invocationFactory = nil
	}
	errs = append(errs, p.closeEngine(ctx))
	return errors.Join(errs...)
}

func (p *Plugin) closeEngine(ctx context.Context) error {
	engine := p.engine
	p.engine = nil
	if engine == nil {
		return nil
	}
	return engine.Close(ctx)
}
