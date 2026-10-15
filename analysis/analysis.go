// Package analysis checks workflow and action sources without selecting a CLI
// output format. Findings are results; an error indicates incomplete analysis.
package analysis

import (
	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/config"
	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/engine"
	"context"
)

// Source is an in-memory workflow or action manifest with optional configuration.
type Source struct {
	Path    string
	Content []byte
	Config  *config.Config
}

// Tool selects a literal executable and arguments. An empty executable disables
// the tool; executable paths are never interpreted as shell command lines.
type Tool struct {
	Executable  string
	Arguments   []string
	Environment []string
	WorkingDir  string
}

// Options controls configuration, external tools and filesystem access.
// ReadFile must support concurrent calls. It does not sandbox external tools.
type Options struct {
	Analyzers      []analyzer.Registration
	WorkingDir     string
	ReadFile       func(string) ([]byte, error)
	ShellCheck     Tool
	Ruff           Tool
	Presets        config.Presets
	IgnorePatterns []string
}

// FileOptions adds filesystem configuration selection to analysis options.
// In-memory Analyze uses Source.Config and has no implicit config discovery.
type FileOptions struct {
	Options
	ConfigFile        string
	ConfigOverlays    []config.Overlay
	SkipProjectConfig bool
}

// Request supplies already-loaded source documents. Their Config fields select
// configuration; filesystem configuration discovery is performed by Files and Repository.
type Request struct {
	Sources []Source
	Options Options
}

// Result contains findings and selected document outlines. It may accompany an
// error when analysis completed only partially. FileCount includes clean sources.
type Result struct {
	Diagnostics    []diagnostic.Diagnostic `json:"diagnostics"`
	Documents      Documents               `json:"documents"`
	Inputs         []string                `json:"inputs"`
	Configurations []config.Report         `json:"configurations"`
	FileCount      int                     `json:"file_count"`
}

// Document is a selected declaration outline, not a complete semantic graph.
type Document = engine.DocumentOutline
type Documents = engine.DocumentOutlines
type Workflow = engine.WorkflowOutline
type Job = engine.JobOutline
type Step = engine.StepOutline
type Action = engine.ActionOutline

// Analyze checks already-loaded sources. It does not perform source discovery.
func Analyze(ctx context.Context, request Request) (*Result, error) {
	sources := make([]engine.SourceUnit, len(request.Sources))
	for i, source := range request.Sources {
		sources[i] = engine.SourceUnit{Path: source.Path, Content: source.Content, Config: source.Config}
	}
	ignore, err := engine.CompileIgnorePatterns(request.Options.IgnorePatterns)
	if err != nil {
		return nil, err
	}
	result, err := engine.Analyze(ctx, engine.AnalysisRequest{
		Analyzers: request.Options.Analyzers, Sources: sources, WorkingDir: request.Options.WorkingDir, ReadFile: request.Options.ReadFile,
		ShellcheckOptions: toolOptions(request.Options.ShellCheck), RuffOptions: toolOptions(request.Options.Ruff),
		RulePresets: request.Options.Presets, IgnorePatterns: ignore,
	})
	return resultView(result), err
}

// Files checks explicit paths with project discovery and configuration loading.
func Files(ctx context.Context, paths []string, options FileOptions) (*Result, error) {
	session, err := engine.NewAnalysisSession(sessionOptions(ctx, options))
	if err != nil {
		return nil, err
	}
	result, err := session.Files(paths, nil)
	return resultView(result), err
}

// Repository discovers and checks workflows and actions under a repository.
func Repository(ctx context.Context, directory string, options FileOptions) (*Result, error) {
	session, err := engine.NewAnalysisSession(sessionOptions(ctx, options))
	if err != nil {
		return nil, err
	}
	result, err := session.Repository(directory)
	return resultView(result), err
}

func toolOptions(tool Tool) *engine.ExternalCommandOptions {
	return &engine.ExternalCommandOptions{Executable: &tool.Executable, Arguments: tool.Arguments, Environment: tool.Environment, WorkingDir: tool.WorkingDir}
}

func sessionOptions(ctx context.Context, options FileOptions) engine.AnalysisOptions {
	return engine.AnalysisOptions{
		Analyzers: options.Analyzers, Context: ctx, WorkingDir: options.WorkingDir, ConfigFile: options.ConfigFile,
		ConfigOverlays: options.ConfigOverlays, SkipProjectConfig: options.SkipProjectConfig,
		ReadFile: options.ReadFile, ShellcheckOptions: toolOptions(options.ShellCheck),
		RuffOptions: toolOptions(options.Ruff), RulePresets: options.Presets,
		IgnorePatterns: options.IgnorePatterns, QuietSelection: true,
	}
}

func resultView(result *engine.AnalysisResult) *Result {
	if result == nil {
		return nil
	}
	return &Result{Diagnostics: result.Diagnostics, Documents: result.Documents, Inputs: result.Inputs, Configurations: result.Configurations, FileCount: result.FileCount()}
}
