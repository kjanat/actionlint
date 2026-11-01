package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/analyzerhost/executor"
	"actionlint.kjanat.dev/v2/internal/plugins/component"
	"actionlint.kjanat.dev/v2/runner"
)

// Options configures a component. Zero values select the runtime defaults.
type Options struct {
	Artifact    ArtifactOptions
	Timeout     time.Duration
	MemoryPages uint32
	// Log receives host log lines and the plugin's stdout and stderr lines.
	Log func(string)
}

func (o Options) internal() component.Options {
	return component.Options{Artifact: artifactOptionsToInternal(o.Artifact), Timeout: o.Timeout, MemoryPages: o.MemoryPages, Log: o.Log}
}

// Limits reports the bounds a component runs under.
type Limits struct {
	Timeout     time.Duration
	MemoryPages uint32
}

// String describes the memory and execution time bounds.
func (l Limits) String() string {
	return fmt.Sprintf("memory %d MiB, time %s", uint64(l.MemoryPages)*64/1024, l.Timeout)
}

// Component is a loaded plugin. Inspect its Has methods before registering its
// roles. Components can implement analyzer, runner-provider and script-engine
// roles together. Callers own its lifetime and release it with Close.
// Runtime errors preserve their text, standard context/fs/io classifications,
// and detached path and unsupported-tool-configuration details. Guest runtime
// concrete error types are not part of this facade's errors.As contract.
type Component struct{ loaded *component.Plugin }

func (p *Component) implementation() *component.Plugin {
	if p.loaded == nil {
		return &component.Plugin{}
	}
	return p.loaded
}

// ArtifactDigest identifies the distributed bytes loaded by this component.
func (p *Component) ArtifactDigest() string { return p.implementation().ArtifactDigest() }

// Path returns the source the component was loaded from.
func (p *Component) Path() string { return p.implementation().Path() }

// Limits returns the component's resource bounds.
func (p *Component) Limits() Limits {
	limits := p.implementation().Limits()
	return Limits{Timeout: limits.Timeout, MemoryPages: limits.MemoryPages}
}

// Close releases the component and its runtime resources.
func (p *Component) Close(ctx context.Context) error {
	return snapshotRuntimeFailure(p.implementation().Close(ctx))
}

// HasAnalyzer reports whether the component implements the analyzer role.
func (p *Component) HasAnalyzer() bool { return p.implementation().HasAnalyzer() }

// Descriptor returns a detached analyzer descriptor, or its zero value.
func (p *Component) Descriptor() analyzer.Descriptor { return p.implementation().Descriptor() }

// Analyze checks one document using the analyzer role.
func (p *Component) Analyze(ctx context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	registration, err := executor.PrepareForwarding(analyzer.Registration{Analyzer: p.implementation(), Options: input.Options}, input.Available)
	if err != nil {
		return nil, snapshotRuntimeFailure(err)
	}
	diagnostics, err := executor.RunOne(ctx, input, registration)
	return diagnostics, snapshotRuntimeFailure(err)
}

// HasRunnerProvider reports whether the runner-provider role is implemented.
func (p *Component) HasRunnerProvider() bool { return p.implementation().HasRunnerProvider() }

// RunnerDescriptor returns a detached runner descriptor, or its zero value.
func (p *Component) RunnerDescriptor() runner.Descriptor {
	return p.implementation().RunnerDescriptor()
}

// ResolveRunners resolves selectors using the runner-provider role.
func (p *Component) ResolveRunners(ctx context.Context, request runner.Request) ([]runner.Answer, error) {
	answers, err := p.implementation().ResolveRunners(ctx, request)
	return answers, snapshotRuntimeFailure(err)
}

// HasScriptEngine reports whether the script-engine role is implemented.
func (p *Component) HasScriptEngine() bool { return p.implementation().HasScriptEngine() }

// ScriptDescriptor returns a detached script-engine descriptor.
func (p *Component) ScriptDescriptor() analyzer.ScriptEngineDescriptor {
	return p.implementation().ScriptDescriptor()
}

// ValidateConfig asks the script engine to validate its configuration.
func (p *Component) ValidateConfig(ctx context.Context, config json.RawMessage) error {
	return snapshotRuntimeFailure(p.implementation().ValidateConfig(ctx, config))
}

// Check checks a sanitized script using the script-engine role.
func (p *Component) Check(ctx context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	diagnostics, err := p.implementation().Check(ctx, request)
	return diagnostics, snapshotRuntimeFailure(err)
}

// ToolIntegration returns a detached optional tool profile, or nil.
func (p *Component) ToolIntegration() *analyzer.ToolIntegration {
	return p.implementation().ToolIntegration()
}

// NegotiateTool negotiates host profile settings and explicit plugin options.
func (p *Component) NegotiateTool(ctx context.Context, config, explicit json.RawMessage) (*analyzer.ToolNegotiation, error) {
	negotiation, err := p.implementation().NegotiateTool(ctx, config, explicit)
	return negotiation, snapshotRuntimeFailure(err)
}
