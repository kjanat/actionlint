// Package core runs core WebAssembly script engines that follow the
// actionlint core JSON memory ABI, version 1, with versioned operations.
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

	"actionlint.kjanat.dev/v2/analyzer"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifact"
	"actionlint.kjanat.dev/v2/internal/plugins/wasmbudget"
	"actionlint.kjanat.dev/v2/internal/scriptcoords"
	"actionlint.kjanat.dev/v2/rule"
	"github.com/kjanat/wacogo"
	"github.com/kjanat/wazero"
	"github.com/kjanat/wazero/api"
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
	Rules            []rule.Descriptor      `json:"rules"`
	MetadataVersion  int                    `json:"metadata_version"`
	ABIVersion       int                    `json:"abi_version"`
	Transport        string                 `json:"transport"`
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	PluginVersion    string                 `json:"plugin_version"`
	Engine           EngineVersion          `json:"engine"`
	Interfaces       map[string][]Interface `json:"interfaces"`
	Capabilities     Capabilities           `json:"capabilities"`
	ConfigSchema     json.RawMessage        `json:"config_schema"`
	TemplateContract string                 `json:"template_contract,omitempty"`
}

// EngineVersion identifies the linked analysis engine.
type EngineVersion struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Capabilities lists what the engine supports. Fixes, ReportedSources and
// Resources take effect only with check version 3; Resources may name only
// analyzer.ToolDirectory.
type Capabilities struct {
	ScriptShells     []string                    `json:"script_shells,omitempty"`
	ScriptInputs     []analyzer.ScriptInput      `json:"script_inputs,omitempty"`
	Check            bool                        `json:"check"`
	ValidateConfig   bool                        `json:"validate_config"`
	ConfigSchema     bool                        `json:"config_schema"`
	Fixes            bool                        `json:"fixes"`
	ReportedSources  bool                        `json:"reported_sources,omitempty"`
	Resources        []analyzer.ToolResourceKind `json:"resources,omitempty"`
	Format           bool                        `json:"format"`
	PositionEncoding string                      `json:"position_encoding"`
	MaxInputBytes    int                         `json:"max_input_bytes"`
}

// Engine is a loaded core module. It implements analyzer.ScriptBackend and
// analyzer.ScriptConfigValidator, pools at most four instances across concurrent
// calls and discards an instance after a trap or an exceeded limit.
type Engine struct {
	path     string
	limits   Limits
	metadata Metadata
	check    string
	// checkVersion is the negotiated check operation version: 1, 2 or 3.
	checkVersion int
	validate     string
	maxInput     int

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
	if metadata.TemplateContract != "" && len(metadata.Capabilities.ScriptShells)+len(metadata.Capabilities.ScriptInputs) == 0 {
		return errors.New("template-aware engines must declare shell or action-input routes")
	}
	for i, shell := range metadata.Capabilities.ScriptShells {
		// Legacy core metadata accepted executable casing and Windows suffixes.
		metadata.Capabilities.ScriptShells[i] = strings.TrimSuffix(strings.ToLower(shell), ".exe")
	}
	if metadata.Interfaces == nil {
		metadata.Interfaces = map[string][]Interface{"check": {{1, "check"}}, "validate_config": {{1, "validate_config"}}, "metadata": {{1, "metadata"}}}
	}
	if metadata.TemplateContract != "" && metadata.TemplateContract != analyzer.TemplateEffectsContract {
		return errors.New("unsupported template effects contract")
	}
	for _, resource := range metadata.Capabilities.Resources {
		if resource != analyzer.ToolDirectory {
			return fmt.Errorf("metadata requests unsupported resource %q; core engines may request %q", resource, analyzer.ToolDirectory)
		}
	}
	exports := e.compiled.ExportedFunctions()
	// resolve returns the export of the first supported version in the
	// host's preference order.
	resolve := func(operation string, versions ...int) (string, int, error) {
		for _, version := range versions {
			for _, candidate := range metadata.Interfaces[operation] {
				if candidate.Version != version {
					continue
				}
				if candidate.Export == "" {
					return "", 0, fmt.Errorf("metadata advertises %s version %d without an export", operation, version)
				}
				definition, ok := exports[candidate.Export]
				if !ok || !slices.Equal(definition.ParamTypes(), requiredExports[operation].params) || !slices.Equal(definition.ResultTypes(), requiredExports[operation].results) {
					return "", 0, fmt.Errorf("metadata advertises %s version %d through export %q, which the module does not provide with that signature", operation, version, candidate.Export)
				}
				return candidate.Export, version, nil
			}
		}
		return "", 0, nil
	}
	// Check 3 carries template effects when the engine declares a template
	// contract, as check 2 does; otherwise it extends check 1.
	checkVersions := []int{3, 1}
	if metadata.TemplateContract != "" {
		checkVersions = []int{3, 2}
	}
	check, version, err := resolve("check", checkVersions...)
	if err != nil {
		return err
	}
	if check == "" {
		if metadata.TemplateContract != "" {
			return errors.New("metadata advertises no check version compatible with its template contract (versions 2 and 3)")
		}
		return errors.New("metadata advertises no check version this host supports (versions 1 and 3)")
	}
	validate, _, err := resolve("validate_config", 1)
	if err != nil {
		return err
	}
	if metadata.Capabilities.MaxInputBytes > 0 {
		e.maxInput = metadata.Capabilities.MaxInputBytes
	}
	e.metadata, e.check, e.checkVersion, e.validate = metadata, check, version, validate
	return nil
}

// CheckVersion returns the negotiated check operation version.
func (e *Engine) CheckVersion() int { return e.checkVersion }

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

// ScriptDescriptor exposes negotiated capabilities to every host entrypoint.
// Fixes, reported sources and resources are capabilities of check 3 only.
func (e *Engine) ScriptDescriptor() analyzer.ScriptEngineDescriptor {
	descriptor := e.Metadata().ScriptDescriptor()
	if e.checkVersion == 3 {
		capabilities := e.metadata.Capabilities
		descriptor.Fixes, descriptor.ReportedSources = capabilities.Fixes, capabilities.ReportedSources
		descriptor.Resources = slices.Clone(capabilities.Resources)
	}
	return descriptor
}

func (m Metadata) ScriptDescriptor() analyzer.ScriptEngineDescriptor {
	descriptor := rule.Descriptor{Name: m.Engine.Name, Description: m.Name, Provider: m.ID}
	for _, candidate := range m.Rules {
		if candidate.Name == m.Engine.Name {
			descriptor = candidate.Clone()
			break
		}
	}
	return analyzer.ScriptEngineDescriptor{ID: m.ID, Version: m.PluginVersion, Name: m.Name,
		Engine: m.Engine.Name, EngineVersion: m.Engine.Version, TemplateContract: m.TemplateContract,
		Shells: slices.Clone(m.Capabilities.ScriptShells), ScriptInputs: slices.Clone(m.Capabilities.ScriptInputs), Rule: descriptor}
}

type response struct {
	OK          bool                        `json:"ok"`
	Diagnostics []analyzer.ScriptDiagnostic `json:"diagnostics"`
	Errors      []string                    `json:"errors"`
}

// checkRequest is the check 1 and check 2 request.
type checkRequest struct {
	Source   string          `json:"source"`
	Filename string          `json:"filename"`
	Config   json.RawMessage `json:"config,omitempty"`
}

// checkRequest3 is the check 3 request. Directory is sent only to engines
// that request the directory resource; it names the host directory and
// grants no access, since core modules import nothing.
type checkRequest3 struct {
	Source    string          `json:"source"`
	Filename  string          `json:"filename"`
	Config    json.RawMessage `json:"config,omitempty"`
	Excluded  []string        `json:"excluded"`
	Directory string          `json:"directory,omitempty"`
}

// encodeRequest encodes request for the negotiated check version.
func (e *Engine) encodeRequest(request analyzer.ScriptRequest) ([]byte, error) {
	if e.checkVersion != 3 {
		return json.Marshal(checkRequest{Source: request.Source, Filename: request.Filename, Config: request.Config})
	}
	payload := checkRequest3{Source: request.Source, Filename: request.Filename, Config: request.Config, Excluded: request.Excluded}
	if payload.Excluded == nil {
		payload.Excluded = []string{}
	}
	if slices.Contains(e.metadata.Capabilities.Resources, analyzer.ToolDirectory) {
		payload.Directory = request.Directory
	}
	return json.Marshal(payload)
}

// Check sends one snippet request and returns its diagnostics.
func (e *Engine) Check(ctx context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	body, err := e.encodeRequest(request)
	if err != nil {
		return nil, err
	}
	result, err := e.call(ctx, e.check, body)
	if err != nil {
		return nil, err
	}
	if !result.OK {
		err = e.engineError(result.Errors)
	}
	valid := result.Diagnostics[:0]
	starts := scriptcoords.Starts(request.Source)
	for i, finding := range result.Diagnostics {
		if e.metadata.TemplateContract != "" && !finding.TemplateEffect.Valid() {
			err = errors.Join(err, fmt.Errorf("engine %s: diagnostic %d has invalid template effect %q", e.name(), i, finding.TemplateEffect))
			continue
		}
		if e.metadata.TemplateContract == "" {
			finding.TemplateEffect = ""
		}
		if e.checkVersion != 3 {
			// Checks 1 and 2 define neither member; an engine sending them
			// is read as before they existed.
			finding.Source, finding.Fixes = "", nil
		} else if problem := e.checkFindings3(finding); problem != "" {
			err = errors.Join(err, fmt.Errorf("engine %s: diagnostic %d %s", e.name(), i, problem))
			continue
		}
		start, end := finding.Location, finding.EndLocation
		if !validRange(start, end) {
			err = errors.Join(err, fmt.Errorf("engine %s: diagnostic %d has invalid range %v-%v", e.name(), i, start, end))
			continue
		}
		valid = append(valid, finding)
		if finding.Source != "" {
			// Positions are the reported file's own, not the snippet's.
			continue
		}
		last := &valid[len(valid)-1]
		last.Location = sourcePosition(starts, last.Location)
		last.EndLocation = sourcePosition(starts, last.EndLocation)
		for f := range last.Fixes {
			replacements := slices.Clone(last.Fixes[f].Replacements)
			for r := range replacements {
				replacement := &replacements[r]
				start := sourcePosition(starts, analyzer.ScriptPosition{Row: replacement.Row, Column: replacement.Column})
				end := sourcePosition(starts, analyzer.ScriptPosition{Row: replacement.EndRow, Column: replacement.EndColumn})
				replacement.Row, replacement.Column, replacement.EndRow, replacement.EndColumn = start.Row, start.Column, end.Row, end.Column
			}
			last.Fixes[f].Replacements = replacements
		}
	}
	return valid, err
}

// checkFindings3 reports why a check 3 diagnostic uses a capability the
// engine did not declare or carries an invalid source or fix, or "".
func (e *Engine) checkFindings3(finding analyzer.ScriptDiagnostic) string {
	capabilities := e.metadata.Capabilities
	if finding.Source != "" {
		switch {
		case !capabilities.ReportedSources:
			return fmt.Sprintf("reports source %q without the reported_sources capability", finding.Source)
		case strings.ContainsRune(finding.Source, 0):
			return fmt.Sprintf("reports invalid source %q", finding.Source)
		case len(finding.Fixes) != 0:
			return fmt.Sprintf("has fixes for reported source %q", finding.Source)
		}
		return ""
	}
	if len(finding.Fixes) != 0 && !capabilities.Fixes {
		return "has fixes without the fixes capability"
	}
	for _, fix := range finding.Fixes {
		if len(fix.Replacements) == 0 {
			return "has a fix without replacements"
		}
		for _, r := range fix.Replacements {
			start, end := analyzer.ScriptPosition{Row: r.Row, Column: r.Column}, analyzer.ScriptPosition{Row: r.EndRow, Column: r.EndColumn}
			if start.Row < 1 || start.Column < 1 || !validRange(start, end) || end == (analyzer.ScriptPosition{}) {
				return fmt.Sprintf("has a fix with invalid range %v-%v", start, end)
			}
		}
	}
	return ""
}

// validRange accepts a positive start and an absent end or one that does not
// precede the start.
func validRange(start, end analyzer.ScriptPosition) bool {
	return start.Row >= 1 && start.Column >= 1 && (end == (analyzer.ScriptPosition{}) ||
		end.Row >= 1 && end.Column >= 1 && (end.Row > start.Row || end.Row == start.Row && end.Column >= start.Column))
}

// sourcePosition converts a snippet position to decoded-source rows; an
// absent or out-of-range position is left unchanged.
func sourcePosition(starts []analyzer.ScriptPosition, position analyzer.ScriptPosition) analyzer.ScriptPosition {
	if position.Row < 1 || position.Row > len(starts) {
		return position
	}
	return scriptcoords.SourcePosition(starts, position)
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
	e.release(ctx, instance)
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

// release returns instance to the idle pool, or closes it when the engine has
// closed. Cleanup keeps ctx values but must finish even after cancellation.
func (e *Engine) release(ctx context.Context, instance api.Module) {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		_ = instance.Close(context.WithoutCancel(ctx))
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
