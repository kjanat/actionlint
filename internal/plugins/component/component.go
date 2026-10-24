// Package component loads typed WebAssembly analyzer, provider and script-engine
// components.
package component

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"actionlint.kjanat.dev/v2/analyzer"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifact"
	hostbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/host"
	annotationsbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/sourceannotations"
	"actionlint.kjanat.dev/v2/runner"
	"github.com/kjanat/wacogo"
	"github.com/kjanat/wacogo/wasmparser"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
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
	descriptor       *analyzer.Descriptor
	runnerDescriptor *runner.Descriptor
	scriptDescriptor *analyzer.ScriptEngineDescriptor

	digest string
	path   string
	limits Limits
	log    func(string)

	engine             *wacogo.Engine
	component          *wacogo.Component
	factory            *hostbind.Factory
	annotationsFactory *annotationsbind.Factory

	gate   *semaphore.Weighted
	memory *memoryBudget
}

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
	if err := checkImports(component.Imports()); err != nil {
		return err
	}
	if err := checkRoleContracts(data, roles.analyzer, roles.runner, roles.script); err != nil {
		return err
	}
	p.component = component
	p.factory, err = hostbind.NewFactory(ctx, p.engine)
	if err != nil {
		return err
	}
	for _, imported := range component.Imports() {
		if wasmparser.CanonicalizeImportName(imported.Name) == wasmparser.CanonicalizeImportName(SourceAnnotationsInterface) {
			p.annotationsFactory, err = annotationsbind.NewFactory(ctx, p.engine)
			if err != nil {
				return err
			}
			break
		}
	}
	if roles.analyzer {
		if err := p.loadAnalyzer(ctx); err != nil {
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

	return nil
}

// ArtifactDigest returns the digest of the loaded snapshot.
func (p *Plugin) ArtifactDigest() string { return p.digest }

// Path returns the component file the plugin was loaded from.
func (p *Plugin) Path() string { return p.path }

// Limits returns the resource bounds for this plugin.
func (p *Plugin) Limits() Limits { return p.limits }

func (p *Plugin) callInterface(ctx context.Context, iface, name string, args ...wacogo.Val) ([]wacogo.Val, error) {
	if err := p.gate.Acquire(ctx, 1); err != nil {
		return nil, p.failure(ctx, err)
	}
	defer p.gate.Release(1)
	ctx, cancel := context.WithTimeout(ctx, p.limits.Timeout)
	defer cancel()
	if p.engine == nil {
		return nil, fmt.Errorf("plugin %s is closed", p.path)
	}
	ctx = experimental.WithMemoryAllocator(ctx, p.memory)
	ctx = wacogo.WithCoreTableAllocator(ctx, p.memory.allocateTables)
	scope := callScope{plugin: p}
	return scope.invoke(ctx, iface, name, args...)
}

func (p *Plugin) failure(ctx context.Context, err error) error {
	name := filepath.Base(p.path)
	if p.descriptor != nil {
		name = p.descriptor.Name
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("plugin %s exceeded its time limit (%s): %w", name, p.limits, errors.Join(context.DeadlineExceeded, err))
	case errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled):
		return fmt.Errorf("plugin %s was cancelled: %w", name, context.Canceled)
	default:
		return fmt.Errorf("plugin %s failed (%s): %w", name, p.limits, err)
	}
}

// Close releases the runtime. The plugin cannot be used afterwards.
func (p *Plugin) Close(ctx context.Context) error {
	if err := p.gate.Acquire(ctx, 1); err != nil {
		return err
	}
	defer p.gate.Release(1)
	var errs []error
	if p.factory != nil {
		errs = append(errs, p.factory.Close(ctx))
		p.factory = nil
	}
	if p.annotationsFactory != nil {
		errs = append(errs, p.annotationsFactory.Close(ctx))
		p.annotationsFactory = nil
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
