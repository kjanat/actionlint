// Package component loads typed WebAssembly analyzer, provider and script-engine
// components.
package component

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

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

const (
	// DefaultTimeout bounds guest execution after caller-controlled queue admission.
	DefaultTimeout = 10 * time.Second
	// DefaultMemoryPages bounds live linear memory and reserved core tables.
	DefaultMemoryPages uint32 = 1024
	maxLiftBytes              = 8 * 1024 * 1024
	maxLiftValues             = 262144
)

// Options configures a loaded plugin. Zero values select the defaults.
type Options struct {
	// Artifact configures URI resolution and verified download caching.
	Artifact    pluginartifact.Options
	Timeout     time.Duration
	MemoryPages uint32
	// Log receives host log lines and the plugin's stdout and stderr lines.
	Log func(string)
}

// Limits reports the resource bounds a plugin runs under.
type Limits struct {
	Timeout     time.Duration
	MemoryPages uint32
}

func (l Limits) String() string {
	return fmt.Sprintf("memory %d MiB, time %s", uint64(l.MemoryPages)*64/1024, l.Timeout)
}

// Plugin is a loaded component. It implements analyzer.Analyzer. Calls are
// serialized; each analysis runs in a fresh instance.
type Plugin struct {
	roles            extensionRoleSet
	runnerDescriptor runner.Descriptor
	scriptDescriptor analyzer.ScriptEngineDescriptor
	digest           string
	path             string
	descriptor       analyzer.Descriptor
	limits           Limits
	log              func(string)

	engine             *wacogo.Engine
	component          *wacogo.Component
	factory            *hostbind.Factory
	annotationsFactory *annotationsbind.Factory

	gate   *semaphore.Weighted
	memory *memoryBudget
}

// Load reads and validates a component and asks it to describe itself.
func Load(ctx context.Context, path string, options Options) (*Plugin, error) {
	if options.MemoryPages > 65536 {
		return nil, fmt.Errorf("plugin memory limit %d exceeds WebAssembly maximum of 65536 pages", options.MemoryPages)
	}
	artifact, err := pluginartifact.Resolve(ctx, path, options.Artifact)
	if err != nil {
		return nil, err
	}
	return LoadArtifact(ctx, artifact, options)
}

// LoadArtifact loads a resolved snapshot without reading its source again.
func LoadArtifact(ctx context.Context, artifact *pluginartifact.Artifact, options Options) (*Plugin, error) {
	path, data := artifact.Info.Source, artifact.Bytes
	if options.MemoryPages > 65536 {
		return nil, fmt.Errorf("plugin memory limit %d exceeds WebAssembly maximum of 65536 pages", options.MemoryPages)
	}
	kind, staticMetadata, err := pluginartifact.Sniff(data)
	if err != nil {
		return nil, err
	}
	if kind != pluginartifact.Component {
		return nil, errors.New("plugin requires a WebAssembly component")
	}
	limits := Limits{Timeout: options.Timeout, MemoryPages: options.MemoryPages}
	if limits.Timeout <= 0 {
		limits.Timeout = DefaultTimeout
	}
	if limits.MemoryPages == 0 {
		limits.MemoryPages = DefaultMemoryPages
	}
	logLine := options.Log
	if logLine == nil {
		logLine = func(string) {}
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
	p.roles = roles
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
	name := p.descriptor.Name
	if name == "" {
		name = filepath.Base(p.path)
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
