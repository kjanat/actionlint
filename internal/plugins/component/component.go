// Package component loads typed WebAssembly analyzer, provider and script-engine
// components.
package component

import (
	"context"
	"errors"

	"actionlint.kjanat.dev/v2/analyzer"
	hostbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/host"
	invocationbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/invocationcontext"
	preparedbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/preparedscripts"
	annotationsbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/sourceannotations"
	filesbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/sourcefiles"
	"actionlint.kjanat.dev/v2/runner"
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
	// A non-nil descriptor identifies a validated, available role.
	analyzerDescriptor *analyzer.Descriptor
	runnerDescriptor   *runner.Descriptor
	scriptDescriptor   *analyzer.ScriptEngineDescriptor
	toolIntegration    *analyzer.ToolIntegration

	digest string
	path   string
	limits Limits
	log    func(string)

	engine             *wacogo.Engine
	component          *wacogo.Component
	hostFactory        *hostbind.Factory
	annotationsFactory *annotationsbind.Factory
	filesFactory       *filesbind.Factory
	preparedFactory    *preparedbind.Factory
	invocationFactory  *invocationbind.Factory

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
	if p.preparedFactory != nil {
		errs = append(errs, p.preparedFactory.Close(ctx))
		p.preparedFactory = nil
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
