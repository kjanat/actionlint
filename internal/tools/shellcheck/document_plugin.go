package shellcheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/internal/tools/selection"
)

// withAnalyzer selects a document analyzer or native execution per document,
// accounting for source and rc-file settings in overrides and composite actions.
func (o Options) withAnalyzer(guest analyzer.Analyzer) (tools.Registration, error) {
	integrator, ok := guest.(analyzer.ToolIntegrator)
	if !ok || integrator.ToolIntegration() == nil || integrator.ToolIntegration().Contract != "kjanat:actionlint/shellcheck-config@0.1.0" || integrator.ToolIntegration().Tool != "shellcheck" {
		return tools.Registration{}, errors.New("analyzer does not advertise the shellcheck configuration contract")
	}
	base := o.Registration()
	defaults := processconfig.Options{Optional: true}
	if o.Process != nil {
		defaults.WorkingDir = o.Process.WorkingDir
		defaults.AllowWasm = o.Process.AllowWasm
	}
	if !processconfig.Enabled(o.Command, o.Process) || o.Backend != nil || o.Runner != nil || o.Settings != nil ||
		o.Command != "shellcheck" || o.Process != nil && !reflect.DeepEqual(*o.Process, defaults) {
		return base, nil
	}
	next := base
	next.Descriptor = guest.Descriptor().Descriptor
	next.NeedsProcess = func(ctx context.Context, cfg, project *config.Config, composite bool) (bool, error) {
		if !base.Enabled(tools.Context{Config: cfg}) {
			return false, nil
		}
		declined, err := declinedConfiguration(ctx, integrator, cfg)
		if err != nil {
			return false, err
		}
		// A referenced composite action can match project overrides that its
		// workflow does not, so a declined override variant still needs native
		// ShellCheck. Variants differ by what the guest negotiates, not only by
		// tool gates as in selection.ConfigCandidates.
		if declined == nil && composite && project != nil && len(project.Overrides) != 0 {
			tool := integrator.ToolIntegration().Tool
			candidates, err := selection.ConfigVariants(project, func(candidate *config.Config) (string, error) {
				return negotiationKey(candidate, tool, base.Enabled(tools.Context{Config: candidate}))
			})
			if err != nil {
				return false, err
			}
			for _, candidate := range candidates {
				if !base.Enabled(tools.Context{Config: candidate}) {
					continue
				}
				if declined, err = declinedConfiguration(ctx, integrator, candidate); err != nil {
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
		if o.ManagedOnly {
			return false, declined
		}
		return base.NeedsProcess(ctx, cfg, project, composite)
	}
	next.Required = func(c tools.Context) bool {
		return true
	}
	next.Build = func(c tools.Context) (tools.Runner, error) {
		return documentPluginRunner{guest: guest, integrator: integrator, context: c, native: func() (tools.Runner, error) {
			if o.ManagedOnly {
				return nil, errors.New("plugin declined configuration; native fallback is not enabled")
			}
			return base.Build(c)
		}}, nil
	}
	return next, nil
}

// declinedConfiguration reports the guest's refusal of cfg. Other negotiation
// failures are returned as errors.
func declinedConfiguration(ctx context.Context, guest analyzer.ToolIntegrator, cfg *config.Config) (*analyzer.UnsupportedToolConfigurationError, error) {
	_, err := negotiateDocument(ctx, guest, cfg, nil)
	if err == nil {
		return nil, nil
	}
	if declined, ok := errors.AsType[*analyzer.UnsupportedToolConfigurationError](err); ok {
		return declined, nil
	}
	return nil, err
}

// negotiationKey identifies everything about cfg that the guest's negotiation
// can depend on before a script is known.
func negotiationKey(cfg *config.Config, tool string, enabled bool) (string, error) {
	options := configuredDocumentOptions(cfg)
	var pluginOptions map[string]any
	if cfg != nil {
		var err error
		if pluginOptions, err = cfg.Lint.Rules.PluginOptions(tool); err != nil {
			return "", err
		}
	}
	data, err := json.Marshal(struct {
		Enabled bool                  `json:"enabled"`
		Backend bool                  `json:"backend"`
		Options documentPluginOptions `json:"options"`
		Plugin  any                   `json:"plugin"`
	}{enabled, cfg != nil && cfg.Tools.Shellcheck.Backend != nil, options, pluginOptions})
	return string(data), err
}

// configuredDocumentOptions reads the ShellCheck settings sent to the guest,
// before script-relative source paths are expanded.
func configuredDocumentOptions(cfg *config.Config) documentPluginOptions {
	options := documentPluginOptions{}
	if cfg == nil {
		return options
	}
	options.RCFile = cfg.Tools.Shellcheck.Config.Path()
	if inline := cfg.Tools.Shellcheck.Config.Inline(); inline != nil {
		options.Shell, options.Disable, options.Enable = inline.Shell, inline.Disable, inline.Enable
		options.ExtendedAnalysis, options.ExternalSources, options.SourcePath = inline.ExtendedAnalysis, inline.ExternalSources, inline.SourcePath
	}
	return options
}

func negotiateDocument(ctx context.Context, guest analyzer.ToolIntegrator, cfg *config.Config, script *tools.Script) (*analyzer.ToolNegotiation, error) {
	if cfg != nil && cfg.Tools.Shellcheck.Backend != nil {
		return nil, &analyzer.UnsupportedToolConfigurationError{Reason: "explicit backend selected"}
	}
	options := configuredDocumentOptions(cfg)
	if script != nil && script.Scope != nil && len(options.SourcePath) != 0 {
		options.SourcePath = slices.Clone(options.SourcePath)
		run := &shellcheckRun{actionPath: script.Scope.ActionPath, paths: runPaths{analysis: script.Scope.AnalysisDir, workspace: script.Scope.Workspace}}
		for i, name := range options.SourcePath {
			expanded, err := run.pathContext().Expand(name)
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

type documentPluginOptions struct {
	Shell            *string  `json:"shell,omitempty"`
	Disable          []string `json:"disable,omitempty"`
	Enable           []string `json:"enable,omitempty"`
	ExtendedAnalysis *bool    `json:"extended-analysis,omitempty"`
	ExternalSources  *bool    `json:"external-sources,omitempty"`
	SourcePath       []string `json:"source-path,omitempty"`
	RCFile           string   `json:"rc-file,omitempty"`
}

type documentPluginRunner struct {
	guest      analyzer.Analyzer
	integrator analyzer.ToolIntegrator
	context    tools.Context
	native     func() (tools.Runner, error)
}

func (r documentPluginRunner) Analyze(ctx context.Context, batch tools.Batch) ([]tools.Finding, error) {
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
		if config.DiagnosticLevel(cfg, "shellcheck") == "off" || cfg != nil && cfg.Tools.Shellcheck.Enabled != nil && !*cfg.Tools.Shellcheck.Enabled {
			continue
		}
		prepared, err := preparePluginScript(script.Fact, nil)
		if err != nil {
			failure = errors.Join(failure, err)
			continue
		}
		if prepared == nil {
			continue
		}
		negotiated, negotiateErr := negotiateDocument(ctx, r.integrator, cfg, &script)
		if script.Scope != nil && script.Scope.Config == cfg {
			negotiatedScopes[script.Scope] = true
		}
		var unsupported *analyzer.UnsupportedToolConfigurationError
		if negotiateErr != nil && !errors.As(negotiateErr, &unsupported) {
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
			mapped, mapErr := files.finding(finding, script, i)
			if mapErr != nil {
				err = errors.Join(err, mapErr)
				continue
			}
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
		if scope == nil || negotiatedScopes[scope] || config.DiagnosticLevel(scope.Config, "shellcheck") == "off" || scope.Config != nil && scope.Config.Tools.Shellcheck.Enabled != nil && !*scope.Config.Tools.Shellcheck.Enabled {
			continue
		}
		negotiatedScopes[scope] = true
		_, err := negotiateDocument(ctx, r.integrator, scope.Config, nil)
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

func (r documentPluginRunner) validateDocumentBatch(batch tools.Batch) error {
	if len(batch.Scripts) == 0 {
		return nil
	}
	if !batch.CallGraph.Valid() {
		return fmt.Errorf("ShellCheck document batch: %w: missing call graph", analyzer.ErrInvalidCallGraph)
	}
	if r.context.Document.Reference != batch.CallGraph.Entry().Reference {
		return fmt.Errorf("ShellCheck document batch: %w: document header disagrees with graph entry", analyzer.ErrInvalidCallGraph)
	}
	for index, script := range batch.Scripts {
		if script.Input != nil {
			continue
		}
		occurrence := script.Fact.Occurrence
		if err := batch.CallGraph.ValidateOccurrence(occurrence); err != nil {
			return fmt.Errorf("ShellCheck document batch script %d: %w", index, err)
		}
		if occurrence.Kind != analyzer.RunScript || script.Fact.Reference != occurrence.Declaration {
			return fmt.Errorf("ShellCheck document batch script %d: %w: missing or foreign prepared occurrence", index, analyzer.ErrInvalidCallGraph)
		}
	}
	return nil
}

func (r documentPluginRunner) input(script tools.Script, graph analyzer.CallGraph) analyzer.Input {
	input := r.context.Document
	input.Available = []analyzer.Fact{analyzer.Source, analyzer.Scripts, analyzer.Invocations, analyzer.Execution}
	input.CallGraph = graph
	input.SelectedScripts = []analyzer.OccurrenceID{script.Fact.Occurrence.ID}
	if graph.Valid() && script.Fact.Occurrence.Owner == graph.Entry().ID {
		input.Scripts = []analyzer.Script{script.Fact}
	} else {
		input.Invoked = []analyzer.Script{script.Fact}
	}
	input.PrepareScript = scriptPreparer([]analyzer.Script{script.Fact})
	return input
}
