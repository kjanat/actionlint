// Package managed runs a tool provider through a managed document analyzer:
// a component that speaks the integration contract the provider's manifest
// names negotiates each configuration and checks the scripts it accepts;
// the provider's own transport checks the rest. The route is deprecated
// and serves providers prepared by the shell profile, whose preparer the
// component imports.
package managed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/internal/tools/backend"
	shellprofile "actionlint.kjanat.dev/v2/internal/tools/profile/shell"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
)

// Bind lets a managed document analyzer replace a provider whose manifest
// names an integration contract. Other providers are returned unchanged.
func Bind(base tools.Registration, p backend.Provider) tools.Registration {
	if p.Manifest.Integration == nil || p.Manifest.Preparation != shellprofile.ID {
		return base
	}
	base.WithAnalyzer = func(guest analyzer.Analyzer) (tools.Registration, error) {
		return withAnalyzer(base, p, guest)
	}
	return base
}

// defaultSelection reports whether a selection runs the provider's default
// command, which a managed analyzer may replace.
func defaultSelection(sel backend.Selection, name string) bool {
	if sel.Backend != nil || sel.Runner != nil || sel.Command != name {
		return false
	}
	if sel.Process == nil {
		return true
	}
	return sel.Process.Optional && !sel.Process.IgnoreConfig && sel.Process.Executable == nil &&
		len(sel.Process.Arguments)+len(sel.Process.LauncherArguments)+len(sel.Process.Environment) == 0
}

// withAnalyzer selects a document analyzer or the provider's transport per
// document, accounting for settings in overrides and composite actions.
func withAnalyzer(base tools.Registration, p backend.Provider, guest analyzer.Analyzer) (tools.Registration, error) {
	m := p.Manifest
	name := m.Rule.Name
	integrator, ok := guest.(analyzer.ToolIntegrator)
	if !ok || integrator.ToolIntegration() == nil || integrator.ToolIntegration().Contract != m.Integration.Contract || integrator.ToolIntegration().Tool != m.Integration.Tool {
		return tools.Registration{}, fmt.Errorf("analyzer does not advertise the %s configuration contract", name)
	}
	sel := p.Selection
	if !defaultSelection(sel, name) {
		return base, nil
	}
	route := documentRoute{name: name, capabilities: m.Capabilities}
	next := base
	next.Descriptor = guest.Descriptor().Descriptor
	next.NeedsProcess = func(ctx context.Context, cfg, project *config.Config, composite bool) (bool, error) {
		if !base.Enabled(tools.Context{Config: cfg}) {
			return false, nil
		}
		declined, err := route.declined(ctx, integrator, cfg)
		if err != nil {
			return false, err
		}
		// A referenced composite action can match project overrides that its
		// workflow does not, so a declined override variant still needs the
		// provider's transport. Variants differ by what the guest negotiates,
		// not only by tool gates as in backend.ConfigCandidates.
		if declined == nil && composite && project != nil && len(project.Overrides) != 0 {
			candidates, err := backend.ConfigVariants(project, func(candidate *config.Config) (string, error) {
				return route.negotiationKey(candidate, base.Enabled(tools.Context{Config: candidate}))
			})
			if err != nil {
				return false, err
			}
			for _, candidate := range candidates {
				if !base.Enabled(tools.Context{Config: candidate}) {
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
		return base.NeedsProcess(ctx, cfg, project, composite)
	}
	next.Required = func(tools.Context) bool { return true }
	next.Build = func(c tools.Context) (tools.Runner, error) {
		return documentRunner{documentRoute: route, guest: guest, integrator: integrator, context: c, native: func() (tools.Runner, error) {
			if sel.ManagedOnly {
				return nil, errors.New("plugin declined configuration; native fallback is not enabled")
			}
			return base.Build(c)
		}}, nil
	}
	return next, nil
}

// documentRoute negotiates one provider's configuration with a guest.
type documentRoute struct {
	name         string
	capabilities provider.Capabilities
}

// enabled reports whether configuration leaves the provider on.
func (r documentRoute) enabled(cfg *config.Config) bool {
	return config.DiagnosticLevel(cfg, r.name) != "off" && (cfg == nil || cfg.Tools[r.name].Enabled == nil || *cfg.Tools[r.name].Enabled)
}

// declined reports the guest's refusal of cfg. Other negotiation failures
// are returned as errors.
func (r documentRoute) declined(ctx context.Context, guest analyzer.ToolIntegrator, cfg *config.Config) (*analyzer.UnsupportedToolConfigurationError, error) {
	_, err := r.negotiate(ctx, guest, cfg, nil)
	if err == nil {
		return nil, nil
	}
	if declined, ok := errors.AsType[*analyzer.UnsupportedToolConfigurationError](err); ok {
		return declined, nil
	}
	return nil, err
}

// negotiationKey identifies everything about cfg that the guest's
// negotiation can depend on before a script is known.
func (r documentRoute) negotiationKey(cfg *config.Config, enabled bool) (string, error) {
	options, err := r.options(cfg)
	if err != nil {
		return "", err
	}
	var pluginOptions map[string]any
	if cfg != nil {
		if pluginOptions, err = cfg.Lint.Rules.PluginOptions(r.name); err != nil {
			return "", err
		}
	}
	data, err := json.Marshal(struct {
		Enabled bool             `json:"enabled"`
		Backend bool             `json:"backend"`
		Options documentSettings `json:"options"`
		Plugin  any              `json:"plugin"`
	}{enabled, cfg != nil && cfg.Tools[r.name].Backend != nil, options, pluginOptions})
	return string(data), err
}

// documentSettings are the provider settings a guest negotiates: the inline
// directives and the native configuration path.
type documentSettings struct {
	shellprofile.Directives
	RCFile string `json:"rc-file,omitempty"`
}

// options reads the settings sent to the guest, before script-relative
// source paths are expanded.
func (r documentRoute) options(cfg *config.Config) (documentSettings, error) {
	var options documentSettings
	if cfg == nil {
		return options, nil
	}
	settings := cfg.Tools[r.name].Config
	options.RCFile = settings.Path()
	if inline := settings.Inline(); inline != nil {
		data, err := json.Marshal(inline)
		if err != nil {
			return documentSettings{}, fmt.Errorf("tools.%s.config: %w", r.name, err)
		}
		directives, err := shellprofile.ParseDirectives(data)
		if err != nil {
			return documentSettings{}, fmt.Errorf("tools.%s.config.%w", r.name, err)
		}
		options.Directives = *directives
	}
	return options, nil
}

func (r documentRoute) negotiate(ctx context.Context, guest analyzer.ToolIntegrator, cfg *config.Config, script *tools.Script) (*analyzer.ToolNegotiation, error) {
	if cfg != nil && cfg.Tools[r.name].Backend != nil {
		return nil, &analyzer.UnsupportedToolConfigurationError{Reason: "explicit backend selected"}
	}
	options, err := r.options(cfg)
	if err != nil {
		return nil, err
	}
	if script != nil && script.Scope != nil && len(options.SourcePath) != 0 {
		options.SourcePath = slices.Clone(options.SourcePath)
		paths := backend.PathContext(&tools.Scope{AnalysisDir: script.Scope.AnalysisDir, Workspace: script.Scope.Workspace, ActionPath: script.Scope.ActionPath}, cfg)
		for i, name := range options.SourcePath {
			expanded, err := paths.Expand(name)
			if err != nil {
				return nil, err
			}
			if expanded != "SCRIPTDIR" && !strings.HasPrefix(expanded, "SCRIPTDIR/") {
				expanded = sourceName(expanded, script.Execution.Directory)
			}
			options.SourcePath[i] = expanded
		}
	}
	data, err := json.Marshal(options)
	if err != nil {
		return nil, err
	}
	var pluginOptions json.RawMessage
	if cfg != nil {
		value, err := cfg.Lint.Rules.PluginOptions(guest.ToolIntegration().Tool)
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
	negotiated, err := guest.NegotiateTool(ctx, data, pluginOptions)
	if err == nil && negotiated == nil {
		return nil, errors.New("tool negotiation returned no result")
	}
	return negotiated, err
}

type documentRunner struct {
	documentRoute
	guest      analyzer.Analyzer
	integrator analyzer.ToolIntegrator
	context    tools.Context
	native     func() (tools.Runner, error)
}

func (r documentRunner) Analyze(ctx context.Context, batch tools.Batch) ([]tools.Finding, error) {
	if err := r.validateDocumentBatch(batch); err != nil {
		return nil, err
	}
	var findings []tools.Finding
	var failure error
	nativeBatch := tools.Batch{CallGraph: batch.CallGraph}
	var nativeIndices []int
	nativeScopes := map[*tools.Scope]bool{}
	negotiatedScopes := map[*tools.Scope]bool{}
	for i, script := range batch.Scripts {
		if err := ctx.Err(); err != nil {
			return findings, errors.Join(failure, err)
		}
		if script.Input != nil {
			continue
		}
		cfg := script.Config
		if !r.enabled(cfg) {
			continue
		}
		prepared, err := shellprofile.PrepareScript(script.Fact, nil)
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
		unsupported, declined := errors.AsType[*analyzer.UnsupportedToolConfigurationError](negotiateErr)
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
		input := r.input(script, batch.CallGraph)
		input.Options = negotiated.Options
		files, fileErr := r.sourceFiles(ctx, script, negotiated.Resources)
		if fileErr != nil {
			failure = errors.Join(failure, fileErr)
			continue
		}
		files.configure(&input)
		result, err := analyzerhost.Run(ctx, input, []analyzer.Registration{{Analyzer: r.guest, Options: input.Options}})
		for _, finding := range result {
			mapped, mapErr := files.finding(r.name, finding, script, i)
			if mapErr != nil {
				err = errors.Join(err, mapErr)
				continue
			}
			mapped.Severity = r.severity(mapped.Severity)
			findings = append(findings, mapped)
		}
		failure = errors.Join(failure, err, files.close())
	}
	// Scopes without analyzed runs still negotiate configuration. Source grants
	// are materialized only for scripts that need them.
	for _, scope := range batch.Scopes {
		if err := ctx.Err(); err != nil {
			return findings, errors.Join(failure, err)
		}
		if scope == nil || negotiatedScopes[scope] || !r.enabled(scope.Config) {
			continue
		}
		negotiatedScopes[scope] = true
		_, err := r.negotiate(ctx, r.integrator, scope.Config, nil)
		if _, unsupported := errors.AsType[*analyzer.UnsupportedToolConfigurationError](err); unsupported {
			if !nativeScopes[scope] {
				nativeScopes[scope] = true
				nativeBatch.Scopes = append(nativeBatch.Scopes, scope)
			}
		} else {
			failure = errors.Join(failure, err)
		}
	}
	if len(nativeIndices) != 0 || len(nativeBatch.Scopes) != 0 {
		native, err := r.native()
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

// severity maps a guest severity through the manifest's table, as the
// provider's own transport does.
func (r documentRunner) severity(severity string) string {
	mapped, listed := r.capabilities.HostSeverity(severity)
	if !listed && r.context.Debug != nil {
		r.context.Debug("%s: mapped unlisted severity %q to %s", r.name, severity, mapped)
	}
	return string(mapped)
}

func (r documentRunner) validateDocumentBatch(batch tools.Batch) error {
	if len(batch.Scripts) == 0 {
		return nil
	}
	if !batch.CallGraph.Valid() {
		return fmt.Errorf("%s document batch: %w: missing call graph", r.name, analyzer.ErrInvalidCallGraph)
	}
	if r.context.Document.Reference != batch.CallGraph.Entry().Reference {
		return fmt.Errorf("%s document batch: %w: document header disagrees with graph entry", r.name, analyzer.ErrInvalidCallGraph)
	}
	for index, script := range batch.Scripts {
		if script.Input != nil {
			continue
		}
		occurrence := script.Fact.Occurrence
		if err := batch.CallGraph.ValidateOccurrence(occurrence); err != nil {
			return fmt.Errorf("%s document batch script %d: %w", r.name, index, err)
		}
		if occurrence.Kind != analyzer.RunScript || script.Fact.Reference != occurrence.Declaration {
			return fmt.Errorf("%s document batch script %d: %w: missing or foreign prepared occurrence", r.name, index, analyzer.ErrInvalidCallGraph)
		}
	}
	return nil
}

func (r documentRunner) input(script tools.Script, graph analyzer.CallGraph) analyzer.Input {
	input := r.context.Document
	input.Available = []analyzer.Fact{analyzer.Source, analyzer.Scripts, analyzer.Invocations, analyzer.Execution}
	input.CallGraph = graph
	input.SelectedScripts = []analyzer.OccurrenceID{script.Fact.Occurrence.ID}
	if graph.Valid() && script.Fact.Occurrence.Owner == graph.Entry().ID {
		input.Scripts = []analyzer.Script{script.Fact}
	} else {
		input.Invoked = []analyzer.Script{script.Fact}
	}
	input.PrepareScript = shellprofile.Preparer([]analyzer.Script{script.Fact})
	return input
}
