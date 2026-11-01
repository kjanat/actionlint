package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"actionlint.kjanat.dev/v2/analyzer"
	corewasm "actionlint.kjanat.dev/v2/internal/plugins/core"
	"actionlint.kjanat.dev/v2/rule"
)

// EngineOptions configures a core script engine. Artifact acquisition and
// compiled-machine-code caches remain independently selectable.
type EngineOptions struct {
	Artifact    ArtifactOptions
	Timeout     time.Duration
	MemoryPages uint32
	// CacheDir stores compiled machine code. Empty disables the cache.
	CacheDir string
}

func (o EngineOptions) internal() corewasm.Options {
	return corewasm.Options{Artifact: artifactOptionsToInternal(o.Artifact), Timeout: o.Timeout, MemoryPages: o.MemoryPages, CacheDir: o.CacheDir}
}

// EngineLimits reports the bounds a core script engine runs under.
type EngineLimits struct {
	Timeout     time.Duration
	MemoryPages uint32
}

// String describes the memory and execution time bounds.
func (l EngineLimits) String() string {
	return fmt.Sprintf("memory %d MiB, time %s", uint64(l.MemoryPages)*64/1024, l.Timeout)
}

// Engine is a loaded core module implementing analyzer.ScriptBackend and
// analyzer.ScriptConfigValidator. Callers own its lifetime.
// Runtime errors use the same detached classification contract as Component.
type Engine struct{ loaded *corewasm.Engine }

func (e *Engine) implementation() *corewasm.Engine {
	if e.loaded == nil {
		return &corewasm.Engine{}
	}
	return e.loaded
}

// CheckVersion returns the negotiated check operation version.
func (e *Engine) CheckVersion() int { return e.implementation().CheckVersion() }

// Path returns the module source the engine was loaded from.
func (e *Engine) Path() string { return e.implementation().Path() }

// Limits returns the engine's resource bounds.
func (e *Engine) Limits() EngineLimits {
	limits := e.implementation().Limits()
	return EngineLimits{Timeout: limits.Timeout, MemoryPages: limits.MemoryPages}
}

// Metadata returns a detached snapshot of the engine's discovery object.
func (e *Engine) Metadata() Metadata { return metadataFromInternal(e.implementation().Metadata()) }

// ScriptDescriptor reports negotiated execution capabilities.
func (e *Engine) ScriptDescriptor() analyzer.ScriptEngineDescriptor {
	return e.implementation().ScriptDescriptor()
}

// Check checks a sanitized script in a bounded engine instance.
func (e *Engine) Check(ctx context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	diagnostics, err := e.implementation().Check(ctx, request)
	return diagnostics, snapshotRuntimeFailure(err)
}

// ValidateConfig asks the engine to validate its configuration.
func (e *Engine) ValidateConfig(ctx context.Context, config json.RawMessage) error {
	return snapshotRuntimeFailure(e.implementation().ValidateConfig(ctx, config))
}

// Close releases the engine and its runtime resources.
func (e *Engine) Close(ctx context.Context) error {
	return snapshotRuntimeFailure(e.implementation().Close(ctx))
}

// Interface is an advertised operation version and its implementing export.
type Interface struct {
	Version int    `json:"version"`
	Export  string `json:"export"`
}

// EngineVersion identifies the linked analysis engine.
type EngineVersion struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Capabilities describes advertised operations; negotiated execution features
// are available through Engine.ScriptDescriptor.
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

// Metadata is the detached discovery object returned by a core engine.
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

// ScriptDescriptor projects advertised metadata into a script descriptor.
// Use Engine.ScriptDescriptor for capabilities negotiated at load time.
func (m Metadata) ScriptDescriptor() analyzer.ScriptEngineDescriptor {
	return m.internal().ScriptDescriptor()
}

func cloneRules(rules []rule.Descriptor) []rule.Descriptor {
	if rules == nil {
		return nil
	}
	result := make([]rule.Descriptor, len(rules))
	for i, descriptor := range rules {
		result[i] = descriptor.Clone()
	}
	return result
}

func metadataFromInternal(m corewasm.Metadata) Metadata {
	var interfaces map[string][]Interface
	if m.Interfaces != nil {
		interfaces = make(map[string][]Interface, len(m.Interfaces))
		for name, versions := range m.Interfaces {
			var converted []Interface
			if versions != nil {
				converted = make([]Interface, len(versions))
			}
			for i, v := range versions {
				converted[i] = Interface{Version: v.Version, Export: v.Export}
			}
			interfaces[name] = converted
		}
	}
	c := m.Capabilities
	return Metadata{Rules: cloneRules(m.Rules), MetadataVersion: m.MetadataVersion, ABIVersion: m.ABIVersion, Transport: m.Transport, ID: m.ID, Name: m.Name, PluginVersion: m.PluginVersion, Engine: EngineVersion{Name: m.Engine.Name, Version: m.Engine.Version}, Interfaces: interfaces, Capabilities: Capabilities{ScriptShells: slices.Clone(c.ScriptShells), ScriptInputs: slices.Clone(c.ScriptInputs), Check: c.Check, ValidateConfig: c.ValidateConfig, ConfigSchema: c.ConfigSchema, Fixes: c.Fixes, ReportedSources: c.ReportedSources, Resources: slices.Clone(c.Resources), Format: c.Format, PositionEncoding: c.PositionEncoding, MaxInputBytes: c.MaxInputBytes}, ConfigSchema: slices.Clone(m.ConfigSchema), TemplateContract: m.TemplateContract}
}

func (m Metadata) internal() corewasm.Metadata {
	var interfaces map[string][]corewasm.Interface
	if m.Interfaces != nil {
		interfaces = make(map[string][]corewasm.Interface, len(m.Interfaces))
		for name, versions := range m.Interfaces {
			var converted []corewasm.Interface
			if versions != nil {
				converted = make([]corewasm.Interface, len(versions))
			}
			for i, v := range versions {
				converted[i] = corewasm.Interface{Version: v.Version, Export: v.Export}
			}
			interfaces[name] = converted
		}
	}
	c := m.Capabilities
	return corewasm.Metadata{Rules: cloneRules(m.Rules), MetadataVersion: m.MetadataVersion, ABIVersion: m.ABIVersion, Transport: m.Transport, ID: m.ID, Name: m.Name, PluginVersion: m.PluginVersion, Engine: corewasm.EngineVersion{Name: m.Engine.Name, Version: m.Engine.Version}, Interfaces: interfaces, Capabilities: corewasm.Capabilities{ScriptShells: slices.Clone(c.ScriptShells), ScriptInputs: slices.Clone(c.ScriptInputs), Check: c.Check, ValidateConfig: c.ValidateConfig, ConfigSchema: c.ConfigSchema, Fixes: c.Fixes, ReportedSources: c.ReportedSources, Resources: slices.Clone(c.Resources), Format: c.Format, PositionEncoding: c.PositionEncoding, MaxInputBytes: c.MaxInputBytes}, ConfigSchema: slices.Clone(m.ConfigSchema), TemplateContract: m.TemplateContract}
}
