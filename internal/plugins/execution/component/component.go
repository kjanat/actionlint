// Package component loads typed WebAssembly analyzer, provider and script-engine
// components.
package component

import (
	"context"
	"errors"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	runner "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	environmentbind "actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint-script/environments"
	shellinputbind "actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint-script/shellscriptinputs"
	hostbind "actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint/host"
	invocationbind "actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint/invocationcontext"
	annotationsbind "actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint/sourceannotations"
	filesbind "actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint/sourcefiles"
	"github.com/kjanat/wacogo"
	"golang.org/x/sync/semaphore"
)

// AnalyzerInterface identifies the document analyzer role.
const AnalyzerInterface = canonicalAnalyzerInterface

// HostInterface is the interface the host implements for plugins.
const HostInterface = hostbind.InterfaceName

// SourceAnnotationsInterface provides optional, source-local YAML comments.
const SourceAnnotationsInterface = annotationsbind.InterfaceName

// Plugin is a loaded component. It implements analyzer.Analyzer. Calls are
// serialized; each analysis runs in a fresh instance.
type Plugin struct {
	reservedRules rule.Names
	// A non-nil descriptor identifies a validated, available role.
	analyzerDescriptor *checkinputs.Descriptor
	runnerDescriptor   *runner.Descriptor
	scriptDescriptor   *checkinputs.ScriptEngineDescriptor
	toolIntegration    *checkinputs.ToolIntegration
	// scriptInterface is the negotiated script-engine version.
	scriptInterface string
	// templateInterface is the negotiated template-effects version, empty
	// without the role. Its check replaces the script engine's.
	templateInterface string

	digest string
	path   string
	limits Limits
	log    func(string)

	engine             *wacogo.Engine
	component          *wacogo.Component
	hostFactory        *hostbind.Factory
	annotationsFactory *annotationsbind.Factory
	filesFactory       *filesbind.Factory
	shellInputFactory  *shellinputbind.Factory
	invocationFactory  *invocationbind.Factory
	environmentFactory *environmentbind.Factory

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
