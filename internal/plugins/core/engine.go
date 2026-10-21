// Package core runs core WebAssembly script engines that follow the
// actionlint-plugin-ruff JSON ABI, version 1.
package core

import (
	"bytes"
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"actionlint.kjanat.dev/analyzer"
	pluginartifact "actionlint.kjanat.dev/internal/plugins/artifact"
	"actionlint.kjanat.dev/internal/plugins/wasmbudget"
	"actionlint.kjanat.dev/rule"
	"github.com/partite-ai/wacogo"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

const (
	// ABIVersion is the core module ABI this host implements.
	ABIVersion uint32 = 1
	// DefaultTimeout bounds one engine call, including admission and initialization.
	DefaultTimeout = 10 * time.Second
	// DefaultMemoryPages bounds linear memory and reserved tables per instance.
	DefaultMemoryPages    uint32 = 4096
	defaultCompileTimeout        = time.Minute
	defaultMaxInputBytes         = 8 * 1024 * 1024
	maxResponseBytes             = 8 * 1024 * 1024
	maxInstances                 = 4
	transport                    = "wasm-core-json"
)

var requiredExports = map[string]struct{ params, results []api.ValueType }{
	"abi_version":     {nil, []api.ValueType{api.ValueTypeI32}},
	"alloc":           {[]api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}},
	"dealloc":         {[]api.ValueType{api.ValueTypeI32}, nil},
	"check":           {[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI64}},
	"validate_config": {[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI64}},
	"metadata":        {nil, []api.ValueType{api.ValueTypeI64}},
}

// Options configures a loaded engine. Zero values select the defaults.
type Options struct {
	// Artifact configures URI resolution and verified download caching.
	Artifact    pluginartifact.Options
	Timeout     time.Duration
	MemoryPages uint32
	// CacheDir stores compiled machine code between runs. Empty disables the cache.
	CacheDir string
}

// Limits reports the resource bounds an engine runs under.
type Limits struct {
	Timeout     time.Duration
	MemoryPages uint32
}

func (l Limits) String() string {
	return fmt.Sprintf("memory %d MiB, time %s", uint64(l.MemoryPages)*64/1024, l.Timeout)
}

// Interface is one advertised operation version and the export that implements it.
type Interface struct {
	Version int    `json:"version"`
	Export  string `json:"export"`
}

// Metadata is the discovery object an engine returns from its metadata export.
type Metadata struct {
	Rules           []rule.Descriptor      `json:"rules"`
	MetadataVersion int                    `json:"metadata_version"`
	ABIVersion      int                    `json:"abi_version"`
	Transport       string                 `json:"transport"`
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	PluginVersion   string                 `json:"plugin_version"`
	Engine          EngineVersion          `json:"engine"`
	Interfaces      map[string][]Interface `json:"interfaces"`
	Capabilities    Capabilities           `json:"capabilities"`
	ConfigSchema    json.RawMessage        `json:"config_schema"`
}

// EngineVersion identifies the linked analysis engine.
type EngineVersion struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Capabilities lists what the engine supports.
type Capabilities struct {
	ScriptShells     []string               `json:"script_shells,omitempty"`
	ScriptInputs     []analyzer.ScriptInput `json:"script_inputs,omitempty"`
	Check            bool                   `json:"check"`
	ValidateConfig   bool                   `json:"validate_config"`
	ConfigSchema     bool                   `json:"config_schema"`
	Fixes            bool                   `json:"fixes"`
	Format           bool                   `json:"format"`
	PositionEncoding string                 `json:"position_encoding"`
	MaxInputBytes    int                    `json:"max_input_bytes"`
}

// Engine is a loaded core module. It implements analyzer.ScriptBackend and
// analyzer.ScriptConfigValidator, pools at most four instances across concurrent
// calls and discards an instance after a trap or an exceeded limit.
type Engine struct {
	path     string
	limits   Limits
	metadata Metadata
	check    string
	validate string
	maxInput int

	runtime  wazero.Runtime
	cache    wazero.CompilationCache
	compiled wazero.CompiledModule

	mu     sync.Mutex
	idle   []api.Module
	closed bool
	active chan struct{}
	done   chan struct{}
}

// Load reads a raw or gzip-compressed core module, checks the version 1
// exports, negotiates the operation versions it advertises and keeps one
// instance ready.
func Load(ctx context.Context, path string, options Options) (*Engine, error) {
	if options.MemoryPages > 65536 {
		return nil, fmt.Errorf("engine memory limit %d exceeds WebAssembly maximum of 65536 pages", options.MemoryPages)
	}
	artifact, err := pluginartifact.Resolve(ctx, path, options.Artifact)
	if err != nil {
		return nil, err
	}
	return LoadArtifact(ctx, artifact, options)
}

// LoadArtifact loads a resolved snapshot without reading its source again.
func LoadArtifact(ctx context.Context, artifact *pluginartifact.Artifact, options Options) (*Engine, error) {
	path, data := artifact.Info.Source, artifact.Bytes
	if options.MemoryPages > 65536 {
		return nil, fmt.Errorf("engine memory limit %d exceeds WebAssembly maximum of 65536 pages", options.MemoryPages)
	}
	kind, metadata, err := pluginartifact.Sniff(data)
	if err != nil {
		return nil, err
	}
	if kind != pluginartifact.Core {
		return nil, fmt.Errorf("engine %s: requires a core WebAssembly module", path)
	}
	if metadata != nil {
		var declared Metadata
		if err := jsonv2.Unmarshal(metadata, &declared); err != nil {
			return nil, fmt.Errorf("custom metadata: %w", err)
		}
		if declared.MetadataVersion != 0 && declared.MetadataVersion != 1 {
			return nil, fmt.Errorf("custom metadata reports unsupported document version %d", declared.MetadataVersion)
		}
		if declared.ABIVersion != int(ABIVersion) || (declared.Transport != "" && declared.Transport != transport) {
			return nil, errors.New("custom metadata advertises an incompatible engine ABI or transport")
		}
	}
	limits := Limits{Timeout: options.Timeout, MemoryPages: options.MemoryPages}
	if limits.Timeout <= 0 {
		limits.Timeout = DefaultTimeout
	}
	if limits.MemoryPages == 0 {
		limits.MemoryPages = DefaultMemoryPages
	}
	tables, err := wacogo.CoreModuleTables(data)
	if err != nil {
		return nil, fmt.Errorf("engine %s: table declarations: %w", path, err)
	}
	tableBytes, err := wasmbudget.Tables(tables, uint64(limits.MemoryPages)<<16)
	if err != nil {
		return nil, fmt.Errorf("engine %s: %w", path, err)
	}
	// Reserve the complete table maxima before allocating any instance. Wasm's
	// own table maxima then bound growth; every pooled instance has this same
	// reduced linear-memory limit.
	tablePages := uint32((tableBytes + (1 << 16) - 1) >> 16)
	config := wazero.NewRuntimeConfig().WithCloseOnContextDone(true).WithMemoryLimitPages(limits.MemoryPages - tablePages)
	var cache wazero.CompilationCache
	if options.CacheDir != "" {
		if cache, err = wazero.NewCompilationCacheWithDir(options.CacheDir); err != nil {
			return nil, fmt.Errorf("engine %s: compilation cache: %w", path, err)
		}
		config = config.WithCompilationCache(cache)
	}
	e := &Engine{path: path, limits: limits, runtime: wazero.NewRuntimeWithConfig(ctx, config), cache: cache, maxInput: defaultMaxInputBytes, active: make(chan struct{}, maxInstances), done: make(chan struct{})}
	if err := e.load(ctx, data, metadata); err != nil {
		_ = e.Close(context.WithoutCancel(ctx))
		return nil, err
	}
	return e, nil
}

func (e *Engine) load(ctx context.Context, data, staticMetadata []byte) error {
	// Native compilation can take longer than one guest call on a cold cache.
	// Bound it independently, while retaining any earlier caller deadline.
	compileCtx, compileCancel := context.WithTimeout(ctx, defaultCompileTimeout)
	defer compileCancel()
	if err := compileCtx.Err(); err != nil {
		return fmt.Errorf("engine %s compilation cancelled: %w", e.name(), err)
	}
	compiled, err := e.runtime.CompileModule(compileCtx, data)
	e.compiled = compiled
	// Compilation cancellation is cooperative. Never initialize a module after
	// its deadline, even when the compiler returns a successful result.
	if ctxErr := compileCtx.Err(); ctxErr != nil {
		err = errors.Join(err, ctxErr)
	}
	if err != nil {
		return fmt.Errorf("engine %s compilation failed (time %s): %w", e.name(), defaultCompileTimeout, err)
	}
	compileCancel()
	ctx, cancel := context.WithTimeout(ctx, e.limits.Timeout)
	defer cancel()
	if err := e.initialize(ctx, staticMetadata); err != nil {
		return e.failure(ctx, err)
	}
	return nil
}

func (e *Engine) initialize(ctx context.Context, staticMetadata []byte) error {
	compiled := e.compiled
	for _, imported := range compiled.ImportedFunctions() {
		module, name, _ := imported.Import()
		return fmt.Errorf("module imports %s.%s; a core engine must not import", module, name)
	}
	if imported := compiled.ImportedMemories(); len(imported) != 0 {
		return errors.New("module imports its memory; a core engine must not import")
	}
	exports := compiled.ExportedFunctions()
	for _, name := range slices.Sorted(maps.Keys(requiredExports)) {
		if name == "check" || name == "validate_config" {
			continue
		}
		signature := requiredExports[name]
		definition, ok := exports[name]
		if !ok {
			return fmt.Errorf("module does not export %s", name)
		}
		if !slices.Equal(definition.ParamTypes(), signature.params) || !slices.Equal(definition.ResultTypes(), signature.results) {
			return fmt.Errorf("export %s has signature %s -> %s; ABI version %d needs %s -> %s", name, typeNames(definition.ParamTypes()), typeNames(definition.ResultTypes()), ABIVersion, typeNames(signature.params), typeNames(signature.results))
		}
	}
	if _, ok := compiled.ExportedMemories()["memory"]; !ok {
		return errors.New("module does not export memory")
	}
	instance, err := e.instantiate(ctx)
	if err != nil {
		return err
	}
	version, err := instance.ExportedFunction("abi_version").Call(ctx)
	if err != nil {
		_ = instance.Close(ctx)
		return err
	}
	if uint32(version[0]) != ABIVersion {
		_ = instance.Close(ctx)
		return fmt.Errorf("module reports ABI version %d; this host implements %d", uint32(version[0]), ABIVersion)
	}
	raw, err := e.invoke(ctx, instance, "metadata", nil)
	if err != nil {
		_ = instance.Close(ctx)
		return err
	}
	if staticMetadata != nil && !pluginartifact.SameMetadata(staticMetadata, raw) {
		_ = instance.Close(ctx)
		return errors.New("actionlint.plugin custom metadata does not match runtime metadata")
	}
	if err := e.negotiate(raw); err != nil {
		_ = instance.Close(ctx)
		return err
	}
	e.idle = append(e.idle, instance)
	return nil
}

func typeNames(types []api.ValueType) string {
	names := make([]string, len(types))
	for i, t := range types {
		names[i] = api.ValueTypeName(t)
	}
	return "[" + strings.Join(names, ", ") + "]"
}

func (e *Engine) negotiate(raw []byte) error {
	var metadata Metadata
	if err := jsonv2.Unmarshal(raw, &metadata); err != nil {
		return fmt.Errorf("metadata: %w", err)
	}
	if metadata.MetadataVersion != 0 && metadata.MetadataVersion != 1 {
		return fmt.Errorf("metadata reports document version %d; this host supports version 1 and legacy unversioned metadata", metadata.MetadataVersion)
	}
	if metadata.ABIVersion != int(ABIVersion) {
		return fmt.Errorf("metadata reports ABI version %d; this host implements %d", metadata.ABIVersion, ABIVersion)
	}
	if metadata.Transport != "" && metadata.Transport != transport {
		return fmt.Errorf("metadata reports transport %q; this host implements %q", metadata.Transport, transport)
	}
	if encoding := metadata.Capabilities.PositionEncoding; encoding != "" && encoding != "unicode" {
		return fmt.Errorf("metadata reports unsupported position encoding %q; this host requires unicode scalar columns", encoding)
	}
	if err := analyzer.ValidateScriptInputs(metadata.Capabilities.ScriptInputs); err != nil {
		return fmt.Errorf("metadata input selectors: %w", err)
	}
	if metadata.Interfaces == nil {
		metadata.Interfaces = map[string][]Interface{"check": {{1, "check"}}, "validate_config": {{1, "validate_config"}}, "metadata": {{1, "metadata"}}}
	}
	exports := e.compiled.ExportedFunctions()
	resolve := func(operation string) (string, error) {
		for _, candidate := range metadata.Interfaces[operation] {
			if candidate.Version != 1 {
				continue
			}
			if candidate.Export == "" {
				return "", fmt.Errorf("metadata advertises %s version 1 without an export", operation)
			}
			definition, ok := exports[candidate.Export]
			if !ok || !slices.Equal(definition.ParamTypes(), requiredExports[operation].params) || !slices.Equal(definition.ResultTypes(), requiredExports[operation].results) {
				return "", fmt.Errorf("metadata advertises %s version 1 through export %q, which the module does not provide with that signature", operation, candidate.Export)
			}
			return candidate.Export, nil
		}
		return "", nil
	}
	check, err := resolve("check")
	if err != nil {
		return err
	}
	if check == "" {
		return errors.New("metadata advertises no check version this host supports (version 1)")
	}
	validate, err := resolve("validate_config")
	if err != nil {
		return err
	}
	if metadata.Capabilities.MaxInputBytes > 0 {
		e.maxInput = metadata.Capabilities.MaxInputBytes
	}
	e.metadata, e.check, e.validate = metadata, check, validate
	return nil
}

// Path returns the module file the engine was loaded from.
func (e *Engine) Path() string { return e.path }

// Limits returns the resource bounds for this engine.
func (e *Engine) Limits() Limits { return e.limits }

// Metadata returns the engine's discovery object.
func (e *Engine) Metadata() Metadata {
	metadata := e.metadata
	metadata.Rules = slices.Clone(metadata.Rules)
	for i := range metadata.Rules {
		metadata.Rules[i] = metadata.Rules[i].Clone()
	}
	metadata.ConfigSchema = slices.Clone(metadata.ConfigSchema)
	metadata.Capabilities.ScriptShells = slices.Clone(metadata.Capabilities.ScriptShells)
	metadata.Capabilities.ScriptInputs = slices.Clone(metadata.Capabilities.ScriptInputs)
	metadata.Interfaces = maps.Clone(metadata.Interfaces)
	for name, versions := range metadata.Interfaces {
		metadata.Interfaces[name] = slices.Clone(versions)
	}
	return metadata
}

type response struct {
	OK          bool                        `json:"ok"`
	Diagnostics []analyzer.ScriptDiagnostic `json:"diagnostics"`
	Errors      []string                    `json:"errors"`
}

// Check sends one snippet request and returns its diagnostics.
func (e *Engine) Check(ctx context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	result, err := e.call(ctx, e.check, body)
	if err != nil {
		return nil, err
	}
	if !result.OK {
		return result.Diagnostics, e.engineError(result.Errors)
	}
	valid := result.Diagnostics[:0]
	for i, finding := range result.Diagnostics {
		start, end := finding.Location, finding.EndLocation
		if start.Row < 1 || start.Column < 1 || end != (analyzer.ScriptPosition{}) &&
			(end.Row < 1 || end.Column < 1 || end.Row < start.Row || end.Row == start.Row && end.Column < start.Column) {
			err = errors.Join(err, fmt.Errorf("engine %s: diagnostic %d has invalid range %v-%v", e.name(), i, start, end))
			continue
		}
		valid = append(valid, finding)
	}
	return valid, err
}

// ValidateConfig asks the engine to validate an options object. Engines that
// advertise no validate_config version 1 accept every configuration.
func (e *Engine) ValidateConfig(ctx context.Context, config json.RawMessage) error {
	if e.validate == "" {
		return nil
	}
	if len(config) == 0 {
		config = json.RawMessage("{}")
	}
	result, err := e.call(ctx, e.validate, config)
	if err != nil {
		return err
	}
	if !result.OK {
		return e.engineError(result.Errors)
	}
	return nil
}

func (e *Engine) engineError(messages []string) error {
	if len(messages) == 0 {
		return fmt.Errorf("engine %s reported a failure without a message", e.name())
	}
	return fmt.Errorf("engine %s: %s", e.name(), strings.Join(messages, "; "))
}

func (e *Engine) name() string {
	if e.metadata.ID != "" {
		return e.metadata.ID
	}
	return filepath.Base(e.path)
}

func (e *Engine) call(ctx context.Context, operation string, body []byte) (*response, error) {
	ctx, cancel := context.WithTimeout(ctx, e.limits.Timeout)
	defer cancel()
	if len(body) > e.maxInput {
		return nil, fmt.Errorf("engine %s: request of %d bytes exceeds the engine limit of %d bytes", e.name(), len(body), e.maxInput)
	}
	select {
	case e.active <- struct{}{}:
	case <-ctx.Done():
		return nil, e.failure(ctx, ctx.Err())
	case <-e.done:
		return nil, fmt.Errorf("engine %s is closed", e.name())
	}
	defer func() { <-e.active }()
	instance, err := e.acquire(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := e.invoke(ctx, instance, operation, body)
	if err != nil {
		_ = instance.Close(context.WithoutCancel(ctx))
		return nil, e.failure(ctx, err)
	}
	e.release(instance)
	var result response
	if err := jsonv2.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("engine %s: %s returned invalid JSON: %w", e.name(), operation, err)
	}
	return &result, nil
}

func (e *Engine) failure(ctx context.Context, err error) error {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("engine %s exceeded its time limit (%s)", e.name(), e.limits)
	case errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled):
		return fmt.Errorf("engine %s was cancelled: %w", e.name(), context.Canceled)
	default:
		return fmt.Errorf("engine %s failed (%s): %w", e.name(), e.limits, err)
	}
}

func (e *Engine) invoke(ctx context.Context, instance api.Module, operation string, body []byte) (output []byte, err error) {
	free := func(pointer uint32) {
		_, cleanupErr := instance.ExportedFunction("dealloc").Call(ctx, uint64(pointer))
		if cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("dealloc: %w", cleanupErr))
		}
	}
	var args []uint64
	if body != nil {
		allocated, err := instance.ExportedFunction("alloc").Call(ctx, uint64(len(body)))
		if err != nil {
			return nil, err
		}
		pointer := uint32(allocated[0])
		if pointer == 0 {
			return nil, fmt.Errorf("engine refused to allocate %d bytes", len(body))
		}
		defer free(pointer)
		if !instance.Memory().Write(pointer, body) {
			return nil, fmt.Errorf("engine memory write of %d bytes at %d failed", len(body), pointer)
		}
		args = []uint64{uint64(pointer), uint64(len(body))}
	}
	results, err := instance.ExportedFunction(operation).Call(ctx, args...)
	if err != nil {
		return nil, err
	}
	pointer, length := uint32(results[0]>>32), uint32(results[0])
	defer free(pointer)
	if length > maxResponseBytes {
		return nil, fmt.Errorf("engine returned %d bytes, exceeding the host response limit of %d bytes", length, maxResponseBytes)
	}
	raw, ok := instance.Memory().Read(pointer, length)
	if !ok {
		return nil, fmt.Errorf("engine returned %d bytes at %d outside its memory", length, pointer)
	}
	return bytes.Clone(raw), nil
}

func (e *Engine) instantiate(ctx context.Context) (api.Module, error) {
	return e.runtime.InstantiateModule(ctx, e.compiled, wazero.NewModuleConfig().WithName(""))
}

func (e *Engine) acquire(ctx context.Context) (api.Module, error) {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil, fmt.Errorf("engine %s is closed", e.name())
	}
	if n := len(e.idle); n > 0 {
		instance := e.idle[n-1]
		e.idle = e.idle[:n-1]
		e.mu.Unlock()
		return instance, nil
	}
	// Instantiation can execute guest start functions. Never hold the pool lock
	// while guest code runs, so another call can cancel or close the runtime.
	runtime, compiled := e.runtime, e.compiled
	e.mu.Unlock()
	instance, err := runtime.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName(""))
	if err != nil {
		return nil, e.failure(ctx, err)
	}
	return instance, nil
}

func (e *Engine) release(instance api.Module) {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		_ = instance.Close(context.Background())
		return
	}
	e.idle = append(e.idle, instance)
	e.mu.Unlock()
}

// Close releases every instance and the runtime. The engine cannot be used afterwards.
func (e *Engine) Close(ctx context.Context) error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	close(e.done)
	idle, runtime, cache := e.idle, e.runtime, e.cache
	e.idle, e.runtime, e.cache = nil, nil, nil
	e.mu.Unlock()
	var errs []error
	for _, instance := range idle {
		errs = append(errs, instance.Close(ctx))
	}
	if runtime != nil {
		errs = append(errs, runtime.Close(ctx))
	}
	if cache != nil {
		errs = append(errs, cache.Close(ctx))
	}
	return errors.Join(errs...)
}
