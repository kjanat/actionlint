package scripts

import (
	"context"
	"errors"
	"fmt"
	"slices"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	scriptexecution "actionlint.kjanat.dev/v2/internal/checking/scripts"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
)

// WithScriptInputs routes run steps through the registration and action inputs
// through the supplied engine.
func WithScriptInputs(registration contract.Registration, descriptor checkinputs.ScriptEngineDescriptor, backend checkinputs.ScriptBackend) (contract.Registration, error) {
	// This registration already binds the selected backend and its complete catalogue.
	registration.ConfiguredBackend = nil
	descriptor.Rule = registration.Descriptor
	shells, err := shellCatalogue(descriptor.Rule, descriptor.Shells)
	if err != nil {
		return contract.Registration{}, err
	}
	registration.RequiresExecution = len(shells) != 0
	descriptor.Shells = nil
	inputs := contract.Registration{Build: func(contract.Context) (contract.Runner, error) { return routingEmpty{}, nil }}
	if len(descriptor.ScriptInputs) != 0 {
		inputs, err = registerEngine(descriptor, backend, engineOptions{enabled: registration.Enabled, prepare: registration.PrepareInput})
	}
	if err != nil {
		return contract.Registration{}, err
	}
	build := registration.Build
	registration.ScriptInputs = slices.Concat(registration.ScriptInputs, inputs.ScriptInputs)
	registration.Build = func(c contract.Context) (contract.Runner, error) {
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

type inputRouting struct{ native, input contract.Runner }

func (r inputRouting) Analyze(ctx context.Context, batch scriptexecution.Batch) ([]contract.ToolFinding, error) {
	native, nativeErr := r.native.Analyze(ctx, batch)
	input, inputErr := r.input.Analyze(ctx, batch)
	return slices.Concat(native, input), errors.Join(nativeErr, inputErr)
}

type routingEmpty struct{}

func (routingEmpty) Analyze(context.Context, scriptexecution.Batch) ([]contract.ToolFinding, error) {
	return nil, nil
}

type selectedRuns struct {
	runner contract.Runner
	shells map[string]struct{}
}

func (r selectedRuns) Analyze(ctx context.Context, batch scriptexecution.Batch) ([]contract.ToolFinding, error) {
	selected, indices := selectScripts(batch, func(script scriptexecution.Unit) bool {
		if script.Input != nil {
			return false
		}
		_, ok := r.shells[scriptShell(script)]
		return ok
	})
	findings, err := r.runner.Analyze(ctx, selected)
	return restoreIndices(findings, indices), err
}

func selectScripts(batch scriptexecution.Batch, accept func(scriptexecution.Unit) bool) (scriptexecution.Batch, []int) {
	selected := scriptexecution.Batch{Scopes: batch.Scopes, CallGraph: batch.CallGraph}
	var indices []int
	for i, script := range batch.Scripts {
		if accept(script) {
			selected.Scripts = append(selected.Scripts, script)
			indices = append(indices, i)
		}
	}
	return selected, indices
}

func restoreIndices(findings []contract.ToolFinding, indices []int) []contract.ToolFinding {
	for i := range findings {
		if findings[i].Script >= 0 && findings[i].Script < len(indices) {
			findings[i].Script = indices[findings[i].Script]
		}
	}
	return findings
}

// Configure binds configured-engine routing once at an entrypoint composition root.
func Configure(registration contract.Registration) contract.Registration {
	load := registration.ConfiguredBackend
	if load == nil {
		return registration
	}
	registration.ConfiguredBackend = nil
	if replace := registration.WithAnalyzer; replace != nil {
		registration.WithAnalyzer = func(guest checkinputs.Checker) (contract.Registration, error) {
			replacement, err := replace(guest)
			if err != nil {
				return contract.Registration{}, err
			}
			return Configure(replacement), nil
		}
	}
	return WithConfiguredEngine(registration, load)
}

// WithConfiguredEngine discovers routes through the same invocation-owned loader
// used by the native adapter, including each composite's effective configuration.
func WithConfiguredEngine(registration contract.Registration, load func(contract.Context) (checkinputs.ScriptBackend, error)) contract.Registration {
	original := registration
	registration.InputSelectors = func(c contract.Context) ([]checkinputs.ScriptInput, error) {
		engine, err := load(c)
		if err != nil {
			return nil, err
		}
		if described, ok := engine.(checkinputs.DescribedScriptBackend); ok {
			descriptor := described.ScriptDescriptor()
			if descriptor.Engine != registration.Name {
				return nil, fmt.Errorf("configured script engine %q does not match tool %q", descriptor.Engine, registration.Name)
			}
			return descriptor.ScriptInputs, nil
		}
		return nil, nil
	}
	registration.Build = func(c contract.Context) (contract.Runner, error) {
		native, err := original.Build(c)
		if err != nil {
			return nil, err
		}
		return configuredRouting{native: native, registration: original, context: c, load: load}, nil
	}
	return registration
}

type configuredRouting struct {
	native       contract.Runner
	registration contract.Registration
	context      contract.Context
	load         func(contract.Context) (checkinputs.ScriptBackend, error)
}

func (r configuredRouting) Analyze(ctx context.Context, batch scriptexecution.Batch) ([]contract.ToolFinding, error) {
	var out []contract.ToolFinding
	var failures error
	groups := map[*configuration.Config]bool{}
	var order []*configuration.Config
	for _, script := range batch.Scripts {
		if !groups[script.Config()] {
			order = append(order, script.Config())
		}
		groups[script.Config()] = true
	}
	nativeScripts := make(map[*configuration.Config]map[string]struct{}, len(order))
	for _, cfg := range order {
		c := r.context
		c.Config, c.ProjectConfig, c.ProjectRoot = cfg, nil, ""
		if cfg != nil {
			resolved := *cfg
			resolved.Overrides = nil
			c.Config = &resolved
		}
		if r.registration.Enabled != nil && !r.registration.Enabled(c) || cfg.RuleSet().DiagnosticLevel(cfg, r.registration.Name) == "off" {
			continue
		}
		engine, err := r.load(c)
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		described, ok := engine.(checkinputs.DescribedScriptBackend)
		if !ok {
			nativeScripts[cfg] = nil
			continue
		}
		descriptor := described.ScriptDescriptor()
		if descriptor.Engine != r.registration.Name {
			failures = errors.Join(failures, fmt.Errorf("configured script engine %q does not match tool %q", descriptor.Engine, r.registration.Name))
			continue
		}
		// Legacy engines without routes inherit the native adapter's selectors.
		if len(descriptor.Shells)+len(descriptor.ScriptInputs) == 0 {
			nativeScripts[cfg] = nil
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
		registration, err := registerEngine(descriptor, engine, engineOptions{enabled: r.registration.Enabled, prepare: r.registration.PrepareInput})
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		input, err := registration.Build(c)
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		selected, indices := selectScripts(batch, func(script scriptexecution.Unit) bool { return script.Config() == cfg })
		findings, err := input.Analyze(ctx, selected)
		out = append(out, restoreIndices(findings, indices)...)
		failures = errors.Join(failures, err)
	}
	// Native adapters validate scope-level configuration even without run snippets.
	selected, indices := selectScripts(batch, func(script scriptexecution.Unit) bool {
		if script.Input != nil {
			return false
		}
		shells, enabled := nativeScripts[script.Config()]
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
