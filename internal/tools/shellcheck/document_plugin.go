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
	next.NeedsProcess = func(cfg, project *config.Config, composite bool) (bool, error) {
		_, err := negotiateDocument(context.Background(), integrator, cfg, nil)
		if err == nil {
			return false, nil
		}
		if _, ok := errors.AsType[*analyzer.UnsupportedToolConfigurationError](err); !ok {
			return false, err
		}
		if o.ManagedOnly {
			return false, err
		}
		return base.NeedsProcess(cfg, project, composite)
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

func negotiateDocument(ctx context.Context, guest analyzer.ToolIntegrator, cfg *config.Config, script *tools.Script) (*analyzer.ToolNegotiation, error) {
	options := documentPluginOptions{}
	if cfg != nil {
		if cfg.Tools.Shellcheck.Backend != nil {
			return nil, &analyzer.UnsupportedToolConfigurationError{Reason: "explicit backend selected"}
		}
		options.RCFile = cfg.Tools.Shellcheck.Config.Path()
		if inline := cfg.Tools.Shellcheck.Config.Inline(); inline != nil {
			options.Shell, options.Disable, options.Enable = inline.Shell, inline.Disable, inline.Enable
			options.ExtendedAnalysis, options.ExternalSources, options.SourcePath = inline.ExtendedAnalysis, inline.ExternalSources, inline.SourcePath
		}
	}
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
		if value := cfg.Lint.Rules.Plugins.Rules[guest.ToolIntegration().Tool].Options; value != nil {
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
	if err := validateDocumentBatch(batch); err != nil {
		return nil, err
	}
	var findings []tools.Finding
	var failure error
	nativeBatch := tools.Batch{CallGraph: batch.CallGraph}
	var nativeIndices []int
	nativeScopes := map[*tools.Scope]bool{}
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
			return findings, errors.Join(failure, err)
		}
		if prepared == nil {
			continue
		}
		negotiated, negotiateErr := negotiateDocument(ctx, r.integrator, cfg, &script)
		var unsupported *analyzer.UnsupportedToolConfigurationError
		if negotiateErr != nil && !errors.As(negotiateErr, &unsupported) {
			return findings, errors.Join(failure, negotiateErr)
		}
		if unsupported != nil {
			nativeBatch.Scripts = append(nativeBatch.Scripts, script)
			nativeIndices = append(nativeIndices, i)
			if !nativeScopes[script.Scope] {
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
			return findings, errors.Join(failure, fileErr)
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
	if len(nativeIndices) != 0 {
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

func validateDocumentBatch(batch tools.Batch) error {
	if len(batch.Scripts) == 0 {
		return nil
	}
	if !batch.CallGraph.Valid() {
		return fmt.Errorf("ShellCheck document batch: %w: missing call graph", analyzer.ErrInvalidCallGraph)
	}
	for index, script := range batch.Scripts {
		if script.Input != nil {
			continue
		}
		occurrence := script.Fact.Occurrence
		if want, exists := batch.CallGraph.Script(occurrence.ID); occurrence.ID == "" || !exists || occurrence.Kind != analyzer.RunScript || occurrence.Declaration != want.Declaration || script.Fact.Reference != want.Declaration {
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
