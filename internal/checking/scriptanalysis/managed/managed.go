// Package managed runs a tool provider through a deprecated managed document analyzer route.
package managed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/results"

	"actionlint.kjanat.dev/v2/internal/checking/executor"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/backends"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/contract"
	"actionlint.kjanat.dev/v2/internal/externallinters/integrationconfig"
	"actionlint.kjanat.dev/v2/internal/externallinters/preparation/shell"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/severity"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
)

func Bind(base contract.Registration, p backends.Provider) contract.Registration {
	if p.Manifest.Integration == nil || p.Manifest.Preparation != shell.ID {
		return base
	}
	base.WithAnalyzer = func(registration results.AnalyzerRegistration) (contract.Registration, error) {
		return withAnalyzer(base, p, registration)
	}
	return base
}

func defaultSelection(sel backends.ToolSelection, name string) bool {
	if sel.Backend != nil || sel.Runner != nil || sel.Command != name {
		return false
	}
	if sel.Process == nil {
		return true
	}
	return sel.Process.Optional && !sel.Process.IgnoreConfig && sel.Process.Executable == nil &&
		len(sel.Process.Arguments)+len(sel.Process.LauncherArguments)+len(sel.Process.Environment) == 0
}

func withAnalyzer(base contract.Registration, p backends.Provider, registration results.AnalyzerRegistration) (contract.Registration, error) {
	m := p.Manifest
	name := m.Rule.Name
	guest := registration.Analyzer
	integrator, ok := guest.(checkinputs.ToolIntegrator)
	if !ok || integrator.ToolIntegration() == nil || integrator.ToolIntegration().Contract != m.Integration.Contract || integrator.ToolIntegration().Tool != m.Integration.Tool {
		return contract.Registration{}, fmt.Errorf("analyzer does not advertise the %s configuration contract", name)
	}
	sel := p.Selection
	if !defaultSelection(sel, name) {
		return base, nil
	}
	settingsResolver, err := integrationconfig.New(m, p.Programs)
	if err != nil {
		return contract.Registration{}, err
	}
	descriptor := guest.Descriptor().Descriptor
	route := documentRoute{name: name, capabilities: m.Capabilities, settingsResolver: settingsResolver, optionsSchema: descriptor.OptionsSchema, analyzerOptions: slices.Clone(registration.Options)}
	next := base
	next.Descriptor = descriptor
	integration := *integrator.ToolIntegration()
	next.ToolIntegration = &integration
	next.NeedsProcess = func(ctx context.Context, cfg, project *configuration.Config) (bool, error) {
		if !base.Enabled(contract.Context{Config: cfg}) {
			return false, nil
		}
		declined, err := route.declined(ctx, integrator, cfg)
		if err != nil {
			return false, err
		}
		// A referenced composite action can match project overrides that its workflow does not match.
		if declined == nil && project != nil && len(project.Overrides) != 0 {
			candidates, err := configuration.ConfigVariants(project, func(candidate *configuration.Config) (string, error) {
				return route.negotiationKey(ctx, candidate, base.Enabled(contract.Context{Config: candidate}))
			})
			if err != nil {
				return false, err
			}
			for _, candidate := range candidates {
				if !base.Enabled(contract.Context{Config: candidate}) {
					continue
				}
				if declined, err = route.declined(ctx, integrator, candidate); err != nil {
					return false, err
				}
				if declined != nil {
					break
				}
			}
		}
		if declined == nil {
			return false, nil
		}
		if sel.ManagedOnly {
			return false, declined
		}
		return base.NeedsProcess(ctx, cfg, project)
	}
	next.Required = func(contract.Context) bool { return true }
	next.Build = func(c contract.Context) (contract.Runner, error) {
		return documentRunner{documentRoute: route, guest: guest, integrator: integrator, context: c, native: func(cfg *configuration.Config) (contract.Runner, error) {
			if sel.ManagedOnly {
				return nil, errors.New("plugin declined configuration; native fallback is not enabled")
			}
			fallback := c
			fallback.Config = cfg
			return base.Build(fallback)
		}}, nil
	}
	return next, nil
}

type documentRoute struct {
	settingsResolver *integrationconfig.SettingsResolver
	name             string
	capabilities     toolmanifest.Capabilities
	optionsSchema    json.RawMessage
	analyzerOptions  json.RawMessage
}

func (r documentRoute) enabled(cfg *configuration.Config) bool {
	return cfg.RuleSet().DiagnosticLevel(cfg, r.name) != "off" && (cfg == nil || cfg.Tools[r.name].Enabled == nil || *cfg.Tools[r.name].Enabled)
}

func (r documentRoute) declined(ctx context.Context, guest checkinputs.ToolIntegrator, cfg *configuration.Config) (*checkinputs.UnsupportedToolConfigurationError, error) {
	_, err := r.negotiate(ctx, guest, cfg, nil)
	if err == nil {
		return nil, nil
	}
	if declined, ok := errors.AsType[*checkinputs.UnsupportedToolConfigurationError](err); ok {
		return declined, nil
	}
	return nil, err
}

func (r documentRoute) negotiationKey(ctx context.Context, cfg *configuration.Config, enabled bool) (string, error) {
	options, err := r.options(ctx, cfg, nil)
	if err != nil {
		return "", err
	}
	var pluginOptions map[string]any
	if cfg != nil {
		if pluginOptions, err = cfg.Lint.Rules.RuleOptions(r.name); err != nil {
			return "", err
		}
	}
	data, err := json.Marshal(struct {
		Enabled bool                               `json:"enabled"`
		Backend bool                               `json:"backend"`
		Options integrationconfig.ResolvedSettings `json:"options"`
		Plugin  any                                `json:"plugin"`
	}{enabled, cfg != nil && cfg.Tools[r.name].Backend != nil, options, pluginOptions})
	return string(data), err
}

func (r documentRoute) options(ctx context.Context, cfg *configuration.Config, expand func(string) (string, error)) (integrationconfig.ResolvedSettings, error) {
	var inline json.RawMessage
	file := ""
	if cfg != nil {
		settings := cfg.Tools[r.name].Config
		file = settings.Path()
		if values := settings.Inline(); values != nil {
			var err error
			inline, err = json.Marshal(values)
			if err != nil {
				return integrationconfig.ResolvedSettings{}, err
			}
		}
	}
	options, err := r.settingsResolver.Resolve(ctx, inline, file, expand)
	if err != nil {
		return integrationconfig.ResolvedSettings{}, fmt.Errorf("tools.%s.config: %w", r.name, err)
	}
	return options, nil
}

func (r documentRoute) negotiate(ctx context.Context, guest checkinputs.ToolIntegrator, cfg *configuration.Config, script *scriptanalysis.Unit) (*checkinputs.ToolNegotiation, error) {
	if cfg != nil && cfg.Tools[r.name].Backend != nil {
		return nil, &checkinputs.UnsupportedToolConfigurationError{Reason: "explicit backend selected"}
	}
	var expand func(string) (string, error)
	if script != nil && script.Scope != nil {
		paths := backends.PathContext(script.Scope)
		expand = paths.Expand
	}
	options, err := r.options(ctx, cfg, expand)
	if err != nil {
		return nil, err
	}
	data := options.Options
	var pluginOptions json.RawMessage
	if cfg != nil {
		value, err := cfg.Lint.Rules.RuleOptions(guest.ToolIntegration().Tool)
		if err != nil {
			return nil, err
		}
		if value != nil {
			pluginOptions, err = json.Marshal(value)
			if err != nil {
				return nil, err
			}
		}
	}
	if len(r.analyzerOptions) != 0 {
		pluginOptions = slices.Clone(r.analyzerOptions)
	}
	if len(pluginOptions) != 0 {
		if err := executor.ValidateOptions(r.optionsSchema, pluginOptions); err != nil {
			return nil, fmt.Errorf("analyzer %q options: %w", r.name, err)
		}
	}
	negotiated, err := guest.NegotiateTool(ctx, data, pluginOptions)
	if err == nil && negotiated == nil {
		return nil, errors.New("tool negotiation returned no result")
	}
	return negotiated, err
}

type documentRunner struct {
	documentRoute
	guest      results.Analyzer
	integrator checkinputs.ToolIntegrator
	context    contract.Context
	native     func(*configuration.Config) (contract.Runner, error)
}

func (r documentRunner) Analyze(ctx context.Context, batch scriptanalysis.Batch) ([]contract.ToolFinding, error) {
	if err := r.validateDocumentBatch(batch); err != nil {
		return nil, err
	}
	var findings []contract.ToolFinding
	var failure error
	nativeBatch := scriptanalysis.Batch{CallGraph: batch.CallGraph}
	var nativeIndices []int
	nativeScopes := map[*scriptanalysis.Scope]bool{}
	negotiatedScopes := map[*scriptanalysis.Scope]bool{}
	for i, script := range batch.Scripts {
		if err := ctx.Err(); err != nil {
			return findings, errors.Join(failure, err)
		}
		if script.Input != nil {
			continue
		}
		cfg := script.Config()
		if !r.enabled(cfg) {
			continue
		}
		fact, _ := batch.Fact(i, true)
		prepared, err := shell.BuildShellScriptInput(fact, nil)
		if err != nil {
			failure = errors.Join(failure, err)
			continue
		}
		if prepared == nil {
			continue
		}
		negotiated, negotiateErr := r.negotiate(ctx, r.integrator, cfg, &script)
		if script.Scope != nil && script.Scope.Config == cfg {
			negotiatedScopes[script.Scope] = true
		}
		unsupported, declined := errors.AsType[*checkinputs.UnsupportedToolConfigurationError](negotiateErr)
		if negotiateErr != nil && !declined {
			failure = errors.Join(failure, negotiateErr)
			continue
		}
		if unsupported != nil {
			nativeBatch.Scripts = append(nativeBatch.Scripts, script)
			nativeIndices = append(nativeIndices, i)
			if script.Scope != nil && !nativeScopes[script.Scope] {
				nativeScopes[script.Scope] = true
				nativeBatch.Scopes = append(nativeBatch.Scopes, script.Scope)
			}
			continue
		}
		if script.Path == "" {
			script.Path, script.Content = r.context.Document.Path, []byte(r.context.Document.Content)
		}
		input := r.input(fact, batch.CallGraph)
		input.Options = negotiated.Options
		files, fileErr := r.sourceFiles(ctx, script, negotiated.Resources)
		if fileErr != nil {
			failure = errors.Join(failure, fileErr)
			continue
		}
		files.configure(&input)
		result, err := executor.Run(ctx, input, []results.AnalyzerRegistration{{Analyzer: r.guest, Options: input.Options}})
		for _, finding := range result {
			mapped, mapErr := files.finding(r.name, finding, script, i)
			if mapErr != nil {
				err = errors.Join(err, mapErr)
				continue
			}
			mapped.Severity = r.severity(string(mapped.Severity))
			findings = append(findings, mapped)
		}
		err = errors.Join(err, files.close())
		failure = errors.Join(failure, err)
		if err == nil && r.context.Completed != nil {
			r.context.Completed(r.name)
		}
	}
	for _, scope := range batch.Scopes {
		if err := ctx.Err(); err != nil {
			return findings, errors.Join(failure, err)
		}
		if scope == nil || negotiatedScopes[scope] || !r.enabled(scope.Config) {
			continue
		}
		negotiatedScopes[scope] = true
		_, err := r.negotiate(ctx, r.integrator, scope.Config, nil)
		if _, unsupported := errors.AsType[*checkinputs.UnsupportedToolConfigurationError](err); unsupported {
			if !nativeScopes[scope] {
				nativeScopes[scope] = true
				nativeBatch.Scopes = append(nativeBatch.Scopes, scope)
			}
		} else {
			failure = errors.Join(failure, err)
		}
	}
	if len(nativeIndices) != 0 || len(nativeBatch.Scopes) != 0 {
		var cfg *configuration.Config
		if len(nativeBatch.Scripts) != 0 {
			cfg = nativeBatch.Scripts[0].Config()
		} else {
			cfg = nativeBatch.Scopes[0].Config
		}
		native, err := r.native(cfg)
		if err != nil {
			return findings, errors.Join(failure, err)
		}
		result, err := native.Analyze(ctx, nativeBatch)
		for j := range result {
			if index := result[j].Script; index >= 0 && index < len(nativeIndices) {
				result[j].Script = nativeIndices[index]
			}
		}
		findings = append(findings, result...)
		failure = errors.Join(failure, err)
	}
	return findings, failure
}

func (r documentRunner) severity(tool string) severity.Severity {
	mapped, listed := r.capabilities.HostSeverity(tool)
	if !listed && r.context.Debug != nil {
		r.context.Debug("%s: mapped unlisted severity %q to %s", r.name, tool, mapped)
	}
	return mapped
}

func (r documentRunner) validateDocumentBatch(batch scriptanalysis.Batch) error {
	if len(batch.Scripts) == 0 {
		return nil
	}
	if !batch.CallGraph.Valid() {
		return fmt.Errorf("%s document batch: %w: missing call graph", r.name, checkinputs.ErrInvalidCallGraph)
	}
	if r.context.Document.Reference != batch.CallGraph.Entry().Reference {
		return fmt.Errorf("%s document batch: %w: document header disagrees with graph entry", r.name, checkinputs.ErrInvalidCallGraph)
	}
	for index, script := range batch.Scripts {
		if script.Input != nil {
			continue
		}
		fact, exists := batch.Fact(index, true)
		if !exists {
			return fmt.Errorf("%s document batch script %d: %w: missing or foreign prepared occurrence", r.name, index, checkinputs.ErrInvalidCallGraph)
		}
		if err := checkinputs.ValidateOccurrence(batch.CallGraph, fact.Occurrence); err != nil {
			return fmt.Errorf("%s document batch script %d: %w", r.name, index, err)
		}
	}
	return nil
}

func (r documentRunner) input(script checkinputs.Script, graph invocation.Graph) checkinputs.AnalyzerInput {
	input := r.context.Document
	input.AvailableInputs = []rule.InputKind{rule.Source, rule.Scripts, rule.Invocations, rule.Execution}
	input.CallGraph = graph
	input.SelectedScripts = []invocation.ID{script.Occurrence.ID}
	if graph.Valid() && script.Occurrence.Owner == graph.RootID() {
		input.Scripts = []checkinputs.Script{script}
	} else {
		input.Invoked = []checkinputs.Script{script}
	}
	input.BuildShellScriptInput = shell.NewShellScriptInputBuilder([]checkinputs.Script{script})
	return input
}
