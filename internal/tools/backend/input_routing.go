package backend

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/tools"
)

// WithScriptInputs retains native run preparation and adds the engine's input-only route.
func WithScriptInputs(registration tools.Registration, descriptor analyzer.ScriptEngineDescriptor, backend analyzer.ScriptBackend) (tools.Registration, error) {
	// This registration already binds the selected backend and its complete catalogue.
	registration.ConfiguredBackend = nil
	descriptor.Rule = registration.Descriptor
	shells, err := shellCatalogue(descriptor.Rule, descriptor.Shells)
	if err != nil {
		return tools.Registration{}, err
	}
	registration.RequiresExecution = len(shells) != 0
	descriptor.Shells = nil
	inputs := tools.Registration{Build: func(tools.Context) (tools.Runner, error) { return routingEmpty{}, nil }}
	if len(descriptor.ScriptInputs) != 0 {
		inputs, err = registerEngine(descriptor, backend, registration.Enabled, registration.PrepareInput)
	}
	if err != nil {
		return tools.Registration{}, err
	}
	build := registration.Build
	registration.ScriptInputs = slices.Concat(registration.ScriptInputs, inputs.ScriptInputs)
	registration.Build = func(c tools.Context) (tools.Runner, error) {
		native, err := build(c)
		if err != nil {
			return nil, err
		}
		input, err := inputs.Build(c)
		if err != nil {
			return nil, err
		}
		return inputRouting{native: selectedRuns{runner: native, shells: shells}, input: input}, nil
	}
	return registration, nil
}

type inputRouting struct{ native, input tools.Runner }

func (r inputRouting) Analyze(ctx context.Context, batch tools.Batch) ([]tools.Finding, error) {
	native, nativeErr := r.native.Analyze(ctx, batch)
	input, inputErr := r.input.Analyze(ctx, batch)
	return slices.Concat(native, input), errors.Join(nativeErr, inputErr)
}

type routingEmpty struct{}

func (routingEmpty) Analyze(context.Context, tools.Batch) ([]tools.Finding, error) { return nil, nil }

type selectedRuns struct {
	runner tools.Runner
	shells map[string]struct{}
}

func (r selectedRuns) Analyze(ctx context.Context, batch tools.Batch) ([]tools.Finding, error) {
	selected, indices := selectScripts(batch, func(script tools.Script) bool {
		if script.Input != nil {
			return false
		}
		_, ok := r.shells[scriptShell(script)]
		return ok
	})
	findings, err := r.runner.Analyze(ctx, selected)
	return restoreIndices(findings, indices), err
}

func selectScripts(batch tools.Batch, accept func(tools.Script) bool) (tools.Batch, []int) {
	selected := tools.Batch{Scopes: batch.Scopes}
	var indices []int
	for i, script := range batch.Scripts {
		if accept(script) {
			selected.Scripts = append(selected.Scripts, script)
			indices = append(indices, i)
		}
	}
	return selected, indices
}
func restoreIndices(findings []tools.Finding, indices []int) []tools.Finding {
	for i := range findings {
		if findings[i].Script >= 0 && findings[i].Script < len(indices) {
			findings[i].Script = indices[findings[i].Script]
		}
	}
	return findings
}

// Configure binds configured-engine routing once at an entrypoint composition root.
func Configure(registration tools.Registration) tools.Registration {
	load := registration.ConfiguredBackend
	if load == nil {
		return registration
	}
	registration.ConfiguredBackend = nil
	return WithConfiguredEngine(registration, load)
}

// WithConfiguredEngine discovers routes through the same invocation-owned loader
// used by the native adapter, including each composite's effective configuration.
func WithConfiguredEngine(registration tools.Registration, load func(tools.Context) (analyzer.ScriptBackend, error)) tools.Registration {
	original := registration
	registration.InputSelectors = func(c tools.Context) ([]analyzer.ScriptInput, error) {
		engine, err := load(c)
		if err != nil {
			return nil, err
		}
		if described, ok := engine.(analyzer.DescribedScriptBackend); ok {
			descriptor := described.ScriptDescriptor()
			if descriptor.Engine != registration.Descriptor.Name {
				return nil, fmt.Errorf("configured script engine %q does not match tool %q", descriptor.Engine, registration.Descriptor.Name)
			}
			return descriptor.ScriptInputs, nil
		}
		return nil, nil
	}
	registration.Build = func(c tools.Context) (tools.Runner, error) {
		native, err := original.Build(c)
		if err != nil {
			return nil, err
		}
		return configuredRouting{native: native, registration: original, context: c, load: load}, nil
	}
	return registration
}

type configuredRouting struct {
	native       tools.Runner
	registration tools.Registration
	context      tools.Context
	load         func(tools.Context) (analyzer.ScriptBackend, error)
}

func (r configuredRouting) Analyze(ctx context.Context, batch tools.Batch) ([]tools.Finding, error) {
	var out []tools.Finding
	var failures error
	groups := map[*config.Config]bool{}
	var order []*config.Config
	for _, script := range batch.Scripts {
		if !groups[script.Config] {
			order = append(order, script.Config)
		}
		groups[script.Config] = true
	}
	nativeScripts := make(map[*config.Config]map[string]struct{}, len(order))
	for _, cfg := range order {
		c := r.context
		c.Config, c.ProjectConfig, c.ProjectRoot = cfg, nil, ""
		if cfg != nil {
			resolved := *cfg
			resolved.Overrides = nil
			c.Config = &resolved
		}
		if r.registration.Enabled != nil && !r.registration.Enabled(c) || config.DiagnosticLevel(cfg, r.registration.Descriptor.Name) == "off" {
			continue
		}
		engine, err := r.load(c)
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		described, ok := engine.(analyzer.DescribedScriptBackend)
		if !ok {
			nativeScripts[cfg] = nil
			continue
		}
		descriptor := described.ScriptDescriptor()
		if descriptor.Engine != r.registration.Descriptor.Name {
			failures = errors.Join(failures, fmt.Errorf("configured script engine %q does not match tool %q", descriptor.Engine, r.registration.Descriptor.Name))
			continue
		}
		shells, err := shellCatalogue(r.registration.Descriptor, descriptor.Shells)
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		nativeScripts[cfg] = shells
		if len(descriptor.ScriptInputs) == 0 {
			continue
		}
		descriptor.Rule = r.registration.Descriptor
		descriptor.Shells = nil
		registration, err := registerEngine(descriptor, engine, r.registration.Enabled, r.registration.PrepareInput)
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		input, err := registration.Build(c)
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		selected, indices := selectScripts(batch, func(script tools.Script) bool { return script.Config == cfg })
		findings, err := input.Analyze(ctx, selected)
		out = append(out, restoreIndices(findings, indices)...)
		failures = errors.Join(failures, err)
	}
	// Native adapters validate scope-level configuration even without run snippets.
	selected, indices := selectScripts(batch, func(script tools.Script) bool {
		if script.Input != nil {
			return false
		}
		shells, enabled := nativeScripts[script.Config]
		if !enabled {
			return false
		}
		if shells == nil {
			return true
		}
		_, supported := shells[scriptShell(script)]
		return supported
	})
	native, err := r.native.Analyze(ctx, selected)
	return slices.Concat(restoreIndices(native, indices), out), errors.Join(failures, err)
}
