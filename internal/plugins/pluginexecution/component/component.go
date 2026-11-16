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

// AnalyzerInterface identifies the document analyzer role.
const AnalyzerInterface = canonicalAnalyzerInterface

// HostInterface is the interface the host implements for plugins.
const HostInterface = host.InterfaceName

// SourceAnnotationsInterface provides optional, source-local YAML comments.
const SourceAnnotationsInterface = sourceannotations.InterfaceName

// Plugin is a loaded component. It implements checkinputs.Checker. Calls are
// serialized; each analysis runs in a fresh instance.
type Plugin struct {
	reservedRules rule.Names
	// A non-nil descriptor identifies a validated, available role.
	analyzerDescriptor *checkinputs.Descriptor
	runnerDescriptor   *runnerapi.Descriptor
	scriptDescriptor   *scriptengine.Descriptor
	toolIntegration    *checkinputs.ToolIntegration
	referenceResolver  bool
	// runner is the negotiated runner-provider version, nil without the
	// role. Runner calls go through it.
	runner runnerProtocol
	// script is the negotiated script-engine version, nil without the role.
	// Script calls go through it; with the template-effects role, its
	// paired template-effects version's check replaces the engine's.
	script scriptEngineProtocol

	digest string
	path   string
	limits Limits
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

// ArtifactDigest returns the digest of the loaded snapshot.
func (p *Plugin) ArtifactDigest() string { return p.digest }

// Path returns the component file the plugin was loaded from.
func (p *Plugin) Path() string { return p.path }

// Limits returns the resource bounds for this plugin.
func (p *Plugin) Limits() Limits { return p.limits }

// Close releases the runtime. The plugin cannot be used afterwards.
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
