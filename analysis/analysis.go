// Package analysis checks workflow and action sources without selecting a CLI
// output format. Findings are results; an error indicates incomplete analysis.
package analysis

import (
	"context"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/config"
	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/document"
	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/engine"
)

// Source is an in-memory workflow or action manifest with optional configuration.
type Source struct {
	Path    string
	Content []byte
	Config  *config.Config
}

// Process selects a literal executable and arguments. An empty executable disables
// the tool; executable paths are never interpreted as shell command lines.
type Process struct {
	Executable  string
	Arguments   []string
	Environment []string
	WorkingDir  string
}

// Tool selects one engine implementation. Backend is independent of transport;
// Process selects the built-in native adapter. A zero Tool disables the engine.
// Supplying both is an error. The caller owns backend lifetime and may reuse it.
type Tool struct {
	Backend analyzer.ScriptBackend
	Process *Process
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
	Documents      document.Outlines       `json:"documents"`
	Inputs         []string                `json:"inputs"`
	Configurations []config.Report         `json:"configurations"`
	FileCount      int                     `json:"file_count"`
}

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
		ShellcheckOptions: backendProcessOptions(request.Options.ShellCheck), RuffOptions: backendProcessOptions(request.Options.Ruff),
		RuffBackend: request.Options.Ruff.Backend, ShellcheckBackend: request.Options.ShellCheck.Backend,
		RulePresets: request.Options.Presets, IgnorePatterns: ignore,
	})
	return resultView(result), err
}

// Files checks explicit paths with project discovery and configuration loading.
func Files(ctx context.Context, paths []string, options FileOptions) (*Result, error) {
	result, err := application.Check(ctx, fileOptions(options), application.Input{Kind: application.Files, Paths: paths})
	return applicationResultView(result), err
}

// Repository discovers and checks workflows and actions under a repository.
func Repository(ctx context.Context, directory string, options FileOptions) (*Result, error) {
	result, err := application.Check(ctx, fileOptions(options), application.Input{Kind: application.Repository, Directory: directory})
	return applicationResultView(result), err
}

func toolOptions(tool Process) *engine.ExternalCommandOptions {
	return &engine.ExternalCommandOptions{Executable: &tool.Executable, LauncherArguments: tool.Arguments, Environment: tool.Environment, WorkingDir: tool.WorkingDir}
}

func fileOptions(options FileOptions) application.Options {
	return application.Options{
		Analyzers: options.Analyzers, WorkingDir: options.WorkingDir, ConfigFile: options.ConfigFile,
		ConfigOverlays: options.ConfigOverlays, SkipProjectConfig: options.SkipProjectConfig,
		ReadFile: options.ReadFile, ShellcheckOptions: backendProcessOptions(options.ShellCheck),
		RuffOptions: backendProcessOptions(options.Ruff), RuffBackend: options.Ruff.Backend,
		ShellcheckBackend: options.ShellCheck.Backend, Presets: options.Presets,
		IgnorePatterns: options.IgnorePatterns,
	}
}

func applicationResultView(result *application.Result) *Result {
	if result == nil {
		return nil
	}
	return &Result{Diagnostics: result.Diagnostics, Documents: result.Documents, Inputs: result.Inputs, Configurations: result.Configurations, FileCount: result.FileCount}
}

func resultView(result *engine.AnalysisResult) *Result {
	if result == nil {
		return nil
	}
	return &Result{Diagnostics: result.Diagnostics, Documents: result.Documents, Inputs: result.Inputs, Configurations: result.Configurations, FileCount: result.FileCount()}
}

func backendProcessOptions(tool Tool) *engine.ExternalCommandOptions {
	if tool.Process != nil {
		return toolOptions(*tool.Process)
	}
	if tool.Backend != nil {
		return nil
	}
	return toolOptions(Process{})
}
