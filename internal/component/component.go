// Package component loads WebAssembly component plugins that implement the
// kjanat:actionlint/analyzer interface and runs them as analyzers.
package component

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
	hostbind "actionlint.kjanat.dev/internal/component/bindings/kjanat/actionlint/host"
	"actionlint.kjanat.dev/internal/fsread"
	"github.com/partite-ai/wacogo"
	"github.com/partite-ai/wacogo/host"
	"github.com/partite-ai/wacogo/wasi"
	"github.com/partite-ai/wacogo/wasi/cli/stdin"
	"github.com/partite-ai/wacogo/wasmparser"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
	"golang.org/x/sync/semaphore"
)

// AnalyzerInterface is the exported interface every plugin must implement.
const AnalyzerInterface = "kjanat:actionlint/analyzer@0.1.0"

// HostInterface is the interface the host implements for plugins.
const HostInterface = hostbind.InterfaceName

const (
	// DefaultTimeout bounds one analyze call.
	DefaultTimeout = 10 * time.Second
	// DefaultMemoryPages bounds a plugin instance's linear memory in 64 KiB pages.
	DefaultMemoryPages uint32 = 1024
	maxComponentBytes         = 128 * 1024 * 1024
)

var grantedImportPrefixes = []string{"wasi:cli/", "wasi:clocks/", "wasi:io/", "wasi:random/", "wasi:filesystem/"}

// Options configures a loaded plugin. Zero values select the defaults.
type Options struct {
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
	path       string
	descriptor analyzer.Descriptor
	limits     Limits
	log        func(string)

	engine    *wacogo.Engine
	component *wacogo.Component
	factory   *hostbind.Factory

	gate   *semaphore.Weighted
	host   *host.ComponentInstance
	world  *wasi.World
	output *lineWriter
}

// Load reads and validates a component and asks it to describe itself.
func Load(ctx context.Context, path string, options Options) (*Plugin, error) {
	if options.MemoryPages > 65536 {
		return nil, fmt.Errorf("plugin %s: memory limit %d exceeds WebAssembly maximum of 65536 pages", path, options.MemoryPages)
	}
	data, err := readComponentFile(ctx, path, maxComponentBytes)
	if err != nil {
		return nil, err
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
	engine := wacogo.NewEngine(ctx, wacogo.WithRuntimeConfig(config))
	p := &Plugin{path: path, limits: limits, log: logLine, engine: engine, gate: semaphore.NewWeighted(1)}
	if err := p.load(ctx, data); err != nil {
		_ = p.Close(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("plugin %s: %w", path, err)
	}
	return p, nil
}

func readComponentFile(ctx context.Context, path string, limit int64) ([]byte, error) {
	file, err := fsread.New(ctx, nil, nil).Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	stop := context.AfterFunc(ctx, func() { _ = file.Close() })
	defer stop()
	info, err := file.Stat()
	if err != nil {
		return nil, errors.Join(err, ctx.Err())
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("component exceeds size limit of %d bytes", limit)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err = errors.Join(err, ctx.Err()); err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("component exceeds size limit of %d bytes", limit)
	}
	return data, nil
}

func (p *Plugin) load(ctx context.Context, data []byte) error {
	component, err := p.engine.LoadComponent(ctx, bytes.NewReader(data))
	if err != nil {
		return err
	}
	if err := checkExports(component.Exports()); err != nil {
		return err
	}
	if err := checkImports(component.Imports()); err != nil {
		return err
	}
	p.component = component
	p.factory, err = hostbind.NewFactory(ctx, p.engine)
	if err != nil {
		return err
	}
	descriptor, err := p.describe(ctx)
	if err != nil {
		return err
	}
	if descriptor.Name == "" {
		return errors.New("plugin describes itself with an empty name")
	}
	p.descriptor = descriptor
	return nil
}

func checkExports(exports []wacogo.ExportDesc) error {
	for _, export := range exports {
		if export.Name == AnalyzerInterface && export.Kind == wacogo.SortInstance {
			return nil
		}
	}
	for _, export := range exports {
		if strings.HasPrefix(export.Name, "kjanat:actionlint/analyzer@") {
			return fmt.Errorf("plugin exports %s; this host supports %s", export.Name, AnalyzerInterface)
		}
	}
	return fmt.Errorf("plugin does not export %s", AnalyzerInterface)
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
	return analyzer.Descriptor{Name: p.descriptor.Name, Requires: append([]analyzer.Fact(nil), p.descriptor.Requires...)}
}

// Analyze runs the plugin on one document in a fresh instance. A trap, an
// exit, an exceeded limit or a refused call is returned as an error.
func (p *Plugin) Analyze(ctx context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	results, err := p.call(ctx, "analyze", lowerInput(input))
	if err != nil {
		return nil, err
	}
	diagnostics, err := liftAnalysis(results[0])
	if err != nil {
		return nil, p.failure(ctx, err)
	}
	return diagnostics, nil
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
	if err := p.gate.Acquire(ctx, 1); err != nil {
		return nil, p.failure(ctx, err)
	}
	defer p.gate.Release(1)
	ctx, cancel := context.WithTimeout(ctx, p.limits.Timeout)
	defer cancel()
	if p.engine == nil {
		return nil, fmt.Errorf("plugin %s is closed", p.path)
	}
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
		_ = guest.Close(ctx)
		if err != nil {
			p.discardImports(ctx)
		}
	}()
	exported := guest.ExportedInstance(AnalyzerInterface)
	if exported == nil {
		return nil, p.failure(ctx, fmt.Errorf("instance does not export %s", AnalyzerInterface))
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
