package shellcheck

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/tools"
)

// withAnalyzer retains native execution where the caller explicitly selected
// process behavior. Configured sources and rc files are selected per document,
// including overrides and invoked composite actions.
func (o Options) withAnalyzer(guest analyzer.Analyzer) (tools.Registration, error) {
	base := o.Registration()
	if !processconfig.Enabled(o.Command, o.Process) || o.Backend != nil || o.Runner != nil || o.Settings != nil ||
		o.Command != "shellcheck" || o.Process != nil && !reflect.DeepEqual(*o.Process, processconfig.Options{Optional: true}) {
		return base, nil
	}
	next := base
	next.NeedsProcess = func(cfg, project *config.Config, composite bool) (bool, error) {
		if SupportsDocumentPlugin(cfg) {
			return false, nil
		}
		return base.NeedsProcess(cfg, project, composite)
	}
	next.Required = func(c tools.Context) bool {
		return SupportsDocumentPlugin(c.Config) || base.Required(c)
	}
	next.Build = func(c tools.Context) (tools.Runner, error) {
		return documentPluginRunner{guest: guest, context: c, help: base.Descriptor.Help, native: func() (tools.Runner, error) { return base.Build(c) }}, nil
	}
	return next, nil
}

// SupportsDocumentPlugin reports whether ShellCheck's effective configuration
// can run without native rc files, external sources or process-specific options.
func SupportsDocumentPlugin(cfg *config.Config) bool {
	if cfg == nil {
		return true
	}
	settings := cfg.Tools.Shellcheck
	if settings.Backend != nil {
		return false
	}
	source := settings.Config
	if source.Path() != "" {
		return false
	}
	if inline := source.Inline(); inline != nil {
		if len(inline.SourcePath) != 0 || inline.ExternalSources != nil && *inline.ExternalSources {
			return false
		}
		// Native ShellCheck accepts ranges and "all"; the component accepts
		// individual diagnostic codes. Preserve that native configuration.
		for _, code := range inline.Disable {
			if code == "all" || strings.Contains(code, "-") {
				return false
			}
		}
	}
	return true
}

type documentPluginOptions struct {
	Shell            *string  `json:"shell,omitempty"`
	Disable          []string `json:"disable,omitempty"`
	Enable           []string `json:"enable,omitempty"`
	ExtendedAnalysis *bool    `json:"extended-analysis,omitempty"`
}

type documentPluginRunner struct {
	guest   analyzer.Analyzer
	context tools.Context
	help    string
	native  func() (tools.Runner, error)
}

func (r documentPluginRunner) Analyze(ctx context.Context, batch tools.Batch) ([]tools.Finding, error) {
	var findings []tools.Finding
	var failure error
	nativeBatch := tools.Batch{}
	var nativeIndices []int
	nativeScopes := map[*tools.Scope]bool{}
	for i, script := range batch.Scripts {
		if err := ctx.Err(); err != nil {
			return findings, errors.Join(failure, err)
		}
		cfg := script.Config
		if config.DiagnosticLevel(cfg, "shellcheck") == "off" || cfg != nil && cfg.Tools.Shellcheck.Enabled != nil && !*cfg.Tools.Shellcheck.Enabled {
			continue
		}
		if !SupportsDocumentPlugin(cfg) {
			nativeBatch.Scripts = append(nativeBatch.Scripts, script)
			nativeIndices = append(nativeIndices, i)
			if !nativeScopes[script.Scope] {
				nativeScopes[script.Scope] = true
				nativeBatch.Scopes = append(nativeBatch.Scopes, script.Scope)
			}
			continue
		}
		if script.Path == "" {
			script.Path, script.Content = r.context.DocumentPath, r.context.DocumentContent
		}
		input, err := r.input(script)
		if err != nil {
			return findings, errors.Join(failure, err)
		}
		result, err := analyzerhost.Run(ctx, input, []analyzer.Registration{{Analyzer: r.guest, Options: input.Options}})
		for _, finding := range result {
			finding.Rule = "shellcheck"
			finding.Help = r.help
			findings = append(findings, tools.Finding{Diagnostic: finding, Script: i, Content: script.Content})
		}
		failure = errors.Join(failure, err)
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

func (r documentPluginRunner) input(script tools.Script) (analyzer.Input, error) {
	options := documentPluginOptions{}
	if script.Config != nil {
		if inline := script.Config.Tools.Shellcheck.Config.Inline(); inline != nil {
			options = documentPluginOptions{Shell: inline.Shell, Disable: inline.Disable, Enable: inline.Enable, ExtendedAnalysis: inline.ExtendedAnalysis}
		}
	}
	data, err := json.Marshal(options)
	if err != nil {
		return analyzer.Input{}, err
	}
	start := diagnostic.Position{Line: script.Value.Line, Column: script.Value.Col}
	supplied := analyzer.Script{Text: script.Text, Path: script.Path, Start: start,
		Runtime: &analyzer.Runtime{Shell: script.Execution.Shell, ShellKnown: script.Execution.Shell != "", ShellImplicit: script.Execution.ImplicitShell, Directory: script.Execution.Directory, DirectoryKnown: script.Execution.DirectoryKnown}}
	if script.Shell != nil {
		supplied.Shell = *script.Shell
	}
	for _, span := range script.Source.LineSpans() {
		supplied.Spans = append(supplied.Spans, analyzer.Span{Line: span.Line, Start: span.Start, End: span.End,
			Document: diagnostic.Position{Line: span.YamlLine, Column: span.YamlColumn}, DocumentEnd: diagnostic.Position{Line: span.YamlEndLine, Column: span.YamlEndColumn}})
	}
	input := analyzer.Input{Path: script.Path, Content: string(script.Content), Kind: "workflow", ParseStatus: "complete",
		Available: []analyzer.Fact{analyzer.Source, analyzer.Scripts, analyzer.Invocations, analyzer.Execution},
		Options:   data}
	if r.context.Reference != nil {
		input.Reference = r.context.Reference(script.Path, script.Content, diagnostic.Position{})
		supplied.Reference = r.context.Reference(script.Path, script.Content, script.Declaration)
	}
	if script.Scope != nil && script.Scope.Composite {
		input.Kind = "action"
	}
	input.Scripts = []analyzer.Script{supplied}
	return input, nil
}
