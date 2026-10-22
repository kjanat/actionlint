// Package component loads typed WebAssembly analyzer, provider and script-engine
// components.
package component

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifact"
	hostbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/host"
	"actionlint.kjanat.dev/v2/runner"
	"github.com/kjanat/wacogo"
	"github.com/kjanat/wacogo/host"
	"github.com/kjanat/wacogo/wasi"
	"github.com/kjanat/wacogo/wasi/cli/stdin"
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

const (
	// DefaultTimeout bounds guest execution after caller-controlled queue admission.
	DefaultTimeout = 10 * time.Second
	// DefaultMemoryPages bounds live linear memory and reserved core tables.
	DefaultMemoryPages uint32 = 1024
	maxLiftBytes              = 8 * 1024 * 1024
	maxLiftValues             = 262144
)

var grantedImportPrefixes = []string{"wasi:cli/", "wasi:clocks/", "wasi:io/", "wasi:random/", "wasi:filesystem/"}

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
	analyzerRole     bool
	runnerRole       bool
	runnerDescriptor runner.Descriptor
	scriptRole       bool
	scriptDescriptor analyzer.ScriptEngineDescriptor
	digest           string
	path             string
	descriptor       analyzer.Descriptor
	limits           Limits
	log              func(string)

	engine    *wacogo.Engine
	component *wacogo.Component
	factory   *hostbind.Factory

	gate   *semaphore.Weighted
	host   *host.ComponentInstance
	world  *wasi.World
	output *lineWriter
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
	p := &Plugin{path: path, digest: artifact.Info.SHA256, limits: limits, log: logLine, engine: engine, gate: semaphore.NewWeighted(1), memory: &memoryBudget{limit: uint64(limits.MemoryPages) << 16}}
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
	p.analyzerRole, p.runnerRole, p.scriptRole = roles.analyzer, roles.runner, roles.script
	p.factory, err = hostbind.NewFactory(ctx, p.engine)
	if err != nil {
		return err
	}
	if roles.analyzer {
		descriptor, err := p.describe(ctx)
		if err != nil {
			return err
		}
		if descriptor.Name == "" {
			return errors.New("plugin describes itself with an empty name")
		}
		if err := validateSuppressionRuleName(descriptor.Name); err != nil {
			return err
		}
		p.descriptor = descriptor
	}
	if roles.runner {
		results, err := p.callInterface(ctx, RunnerInterface, "describe")
		if err != nil {
			return err
		}
		p.runnerDescriptor, err = liftRunnerDescriptor(results[0])
		if err != nil {
			return err
		}
	}
	if roles.script {
		results, err := p.callInterface(ctx, ScriptEngineInterface, "describe")
		if err != nil {
			return err
		}
		p.scriptDescriptor, err = liftScriptDescriptor(results[0])
		if err != nil {
			return err
		}
		if err := p.verifyScriptMetadata(staticMetadata); err != nil {
			return err
		}
	}

	return nil
}

func validateSuppressionRuleName(name string) error {
	if strings.TrimSpace(name) != name || strings.ContainsAny(name, ",\r\n") ||
		strings.Contains(name, " -- ") || strings.HasSuffix(name, " --") {
		return fmt.Errorf("plugin name %q cannot be used as an inline suppression rule", name)
	}
	return nil
}

type extensionRoleSet struct {
	analyzer bool
	runner   bool
	script   bool
}

func extensionRoles(exports []wacogo.ExportDesc) (extensionRoleSet, error) {
	var roles extensionRoleSet
	supported := []struct {
		name string
		role *bool
	}{{AnalyzerInterface, &roles.analyzer}, {RunnerInterface, &roles.runner}, {ScriptEngineInterface, &roles.script}}
	for _, export := range exports {
		for _, candidate := range supported {
			prefix, _, _ := strings.Cut(candidate.name, "@")
			if !strings.HasPrefix(export.Name, prefix+"@") {
				continue
			}
			if export.Name != candidate.name {
				return roles, fmt.Errorf("plugin exports %s; this host supports %s", export.Name, candidate.name)
			}
			if export.Kind != wacogo.SortInstance {
				return roles, fmt.Errorf("plugin export %s must be an interface instance", export.Name)
			}
			*candidate.role = true
		}
	}
	if !roles.analyzer && !roles.runner && !roles.script {
		return roles, fmt.Errorf("plugin does not export %s, %s or %s", AnalyzerInterface, RunnerInterface, ScriptEngineInterface)
	}
	return roles, nil
}

func checkImports(imports []wacogo.ImportDesc) error {
	hostName := wasmparser.CanonicalizeImportName(HostInterface)
	for _, imported := range imports {
		if wasmparser.CanonicalizeImportName(imported.Name) == hostName {
			continue
		}
		if strings.HasPrefix(imported.Name, "kjanat:actionlint/host@") {
			return fmt.Errorf("plugin imports %s; this host provides %s", imported.Name, HostInterface)
		}
		granted := false
		for _, prefix := range grantedImportPrefixes {
			if strings.HasPrefix(imported.Name, prefix) {
				granted = true
				break
			}
		}
		if !granted {
			return fmt.Errorf("plugin imports %s, which this host does not grant", imported.Name)
		}
	}
	return nil
}

// Path returns the component file the plugin was loaded from.
func (p *Plugin) Path() string { return p.path }

// Limits returns the resource bounds for this plugin.
func (p *Plugin) Limits() Limits { return p.limits }

// Descriptor returns the identity the plugin declared when it was loaded.
func (p *Plugin) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Descriptor: p.descriptor.Clone(), Requires: append([]analyzer.Fact(nil), p.descriptor.Requires...)}
}

// Analyze runs the plugin on one document in a fresh instance. A trap, an
// exit, an exceeded limit or a refused call is returned as an error.
func (p *Plugin) Analyze(ctx context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	if !p.analyzerRole {
		return nil, errors.New("component does not implement the analyzer role")
	}
	results, err := p.call(ctx, "analyze", lowerInput(input))
	if err != nil {
		return nil, err
	}
	diagnostics, err := liftAnalysis(results[0])
	valid := diagnostics[:0]
	for _, finding := range diagnostics {
		if finding.Rule != "" && finding.Rule != p.descriptor.Name {
			err = errors.Join(err, fmt.Errorf("undeclared diagnostic rule %q; use code for plugin-specific check identifiers", finding.Rule))
			continue
		}
		valid = append(valid, finding)
	}
	if err != nil {
		return valid, p.failure(ctx, err)
	}
	return valid, nil
}

func (p *Plugin) describe(ctx context.Context) (analyzer.Descriptor, error) {
	results, err := p.call(ctx, "describe")
	if err != nil {
		return analyzer.Descriptor{}, err
	}
	descriptor, err := liftDescriptor(results[0])
	if err != nil {
		return analyzer.Descriptor{}, p.failure(ctx, err)
	}
	return descriptor, nil
}

func (p *Plugin) call(ctx context.Context, name string, args ...wacogo.Val) (results []wacogo.Val, err error) {
	return p.callInterface(ctx, AnalyzerInterface, name, args...)
}

func (p *Plugin) callInterface(ctx context.Context, iface, name string, args ...wacogo.Val) (results []wacogo.Val, err error) {
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
	defer func() {
		if recovered := recover(); recovered != nil {
			recoveredError, ok := recovered.(error)
			if !ok || !errors.Is(recoveredError, errMemoryBudget) {
				panic(recovered)
			}
			cleanup := context.WithoutCancel(ctx)
			p.discardImports(cleanup)
			results, err = nil, p.failure(ctx, errors.Join(errMemoryBudget, p.engine.Close(cleanup)))
			p.engine = nil
		}
	}()
	if err := p.prepareImports(ctx); err != nil {
		return nil, p.failure(ctx, err)
	}
	output := p.output
	output.reset()
	defer func() {
		output.flush()
		if outputErr := output.err; outputErr != nil {
			err = errors.Join(err, p.failure(ctx, outputErr))
			p.discardImports(ctx)
		}
	}()
	// wacogo's stdin component hands out one stream handle per host instance.
	input, err := stdin.NewInstance(ctx, p.engine, p.world.Streams, p.world.Error, p.world.Poll, nil)
	if err != nil {
		p.discardImports(ctx)
		return nil, p.failure(ctx, err)
	}
	defer func() { _ = input.Close(ctx) }()
	imports := append([]wacogo.InstantiateOption{wacogo.WithInstanceImport(HostInterface, p.host.Core())}, p.world.Imports()...)
	imports = append(imports, wacogo.WithInstanceImport(stdin.InterfaceName, input.Core()))
	guest, err := p.component.Instantiate(ctx, imports...)
	if err != nil {
		p.discardImports(ctx)
		return nil, p.failure(ctx, fmt.Errorf("instantiate: %w", err))
	}
	defer func() {
		if closeErr := guest.Close(ctx); closeErr != nil {
			p.discardImports(ctx)
			// Guest destructors use the call deadline; runtime teardown runs no Wasm.
			closeErr = errors.Join(closeErr, p.engine.Close(context.WithoutCancel(ctx)))
			p.engine = nil
			err = errors.Join(err, p.failure(ctx, fmt.Errorf("close component: %w", closeErr)))
		}
		if err != nil {
			p.discardImports(ctx)
		}
	}()
	exported := guest.ExportedInstance(iface)
	if exported == nil {
		return nil, p.failure(ctx, fmt.Errorf("instance does not export %s", iface))
	}
	fn := exported.ExportedFunc(name)
	if fn == nil {
		return nil, p.failure(ctx, fmt.Errorf("plugin does not export %s", name))
	}
	results, err = fn.Call(ctx, args...)
	if err != nil {
		return nil, p.failure(ctx, err)
	}
	if len(results) != 1 {
		return nil, p.failure(ctx, fmt.Errorf("%s returned %d values", name, len(results)))
	}
	return results, nil
}

func (p *Plugin) prepareImports(ctx context.Context) error {
	if p.host != nil && p.world != nil {
		return nil
	}
	p.discardImports(ctx)
	output := &lineWriter{log: p.log}
	hostInstance, err := p.factory.NewInstance(ctx, hostImplementation{output: output}, nil)
	if err != nil {
		return err
	}
	world, err := wasi.NewWorld(ctx, p.engine, &wasi.Config{Args: []string{filepath.Base(p.path)}, Stdout: output, Stderr: output})
	if err != nil {
		_ = hostInstance.Close(ctx)
		return err
	}
	p.host, p.world, p.output = hostInstance, world, output
	return nil
}

func (p *Plugin) discardImports(ctx context.Context) {
	if p.world != nil {
		_ = p.world.Close(ctx)
		p.world = nil
	}
	if p.host != nil {
		_ = p.host.Close(ctx)
		p.host = nil
	}
	p.output = nil
}

func (p *Plugin) failure(ctx context.Context, err error) error {
	name := p.descriptor.Name
	if name == "" {
		name = filepath.Base(p.path)
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("plugin %s exceeded its time limit (%s)", name, p.limits)
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
	p.discardImports(ctx)
	var errs []error
	if p.factory != nil {
		errs = append(errs, p.factory.Close(ctx))
		p.factory = nil
	}
	if p.engine != nil {
		errs = append(errs, p.engine.Close(ctx))
		p.engine = nil
	}
	return errors.Join(errs...)
}

type hostImplementation struct {
	output *lineWriter
}

func (h hostImplementation) Log(_ context.Context, message string) error {
	// Preserve log-call boundaries without joining a partial stdio line.
	h.output.flush()
	_, err := h.output.Write([]byte(message))
	h.output.flush()
	return err
}

type lineWriter struct {
	log     func(string)
	pending []byte
	written int
	err     error
}

// Output limits cover retained partial lines and total bytes per call,
// including guest-handled I/O failures.
const (
	maxPluginOutput = 1 << 20
	maxPluginLine   = 64 << 10
)

func (w *lineWriter) Write(data []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if len(data) > maxPluginOutput-w.written {
		w.err = fmt.Errorf("plugin output exceeds %d bytes per call", maxPluginOutput)
		return 0, w.err
	}
	w.written += len(data)
	n := len(data)
	for len(data) > 0 {
		count := min(len(data), maxPluginLine-len(w.pending))
		if index := bytes.IndexByte(data[:count], '\n'); index >= 0 {
			w.pending = append(w.pending, data[:index]...)
			w.flush()
			data = data[index+1:]
			continue
		}
		w.pending = append(w.pending, data[:count]...)
		data = data[count:]
		if len(w.pending) == maxPluginLine {
			w.flush()
		}
	}
	return n, nil
}

func (w *lineWriter) flush() {
	if len(w.pending) != 0 && w.log != nil {
		w.log(string(w.pending))
	}
	w.pending = nil
}

func (w *lineWriter) reset() {
	w.pending, w.written, w.err = nil, 0, nil
}
