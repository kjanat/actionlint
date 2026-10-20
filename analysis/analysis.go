// Package analysis checks workflow and action sources without selecting a CLI
// output format. Findings are results; an error indicates incomplete analysis.
package analysis

import (
	"context"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/config"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/document"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
	"actionlint.kjanat.dev/v2/internal/workspace"
	"actionlint.kjanat.dev/v2/resolver"
	"actionlint.kjanat.dev/v2/runner"
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
	Runners        runner.Options
	Resolver       resolver.Resolver
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
	RunnerEvidence []runner.Report         `json:"runner_evidence"`
	Diagnostics    []diagnostic.Diagnostic `json:"diagnostics"`
	Documents      document.Outlines       `json:"documents"`
	Inputs         []string                `json:"inputs"`
	Configurations []config.Report         `json:"configurations"`
	FileCount      int                     `json:"file_count"`
}

// Analyze checks already-loaded sources. It does not perform source discovery.
func Analyze(ctx context.Context, request Request) (*Result, error) {
	sources := make([]workspace.Source, len(request.Sources))
	for i, source := range request.Sources {
		sources[i] = workspace.Source{Path: source.Path, Content: source.Content, Config: source.Config}
	}
	result, err := application.Analyze(ctx, fileOptions(FileOptions{Options: request.Options}), sources)
	return applicationResultView(result), err
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

func toolOptions(tool Process) *processconfig.Options {
	return &processconfig.Options{Executable: &tool.Executable, LauncherArguments: tool.Arguments, Environment: tool.Environment, WorkingDir: tool.WorkingDir}
}

func fileOptions(options FileOptions) application.Options {
	return application.Options{
		Resolver: options.Resolver, Analyzers: options.Analyzers, Runners: options.Runners, WorkingDir: options.WorkingDir, ConfigFile: options.ConfigFile,
		ConfigOverlays: options.ConfigOverlays, SkipProjectConfig: options.SkipProjectConfig,
		ReadFile: options.ReadFile,

		Presets:        options.Presets,
		IgnorePatterns: options.IgnorePatterns, Tools: adapters.Options{ShellcheckOptions: backendProcessOptions(options.ShellCheck),
			RuffOptions: backendProcessOptions(options.Ruff), RuffBackend: options.Ruff.Backend,
			ShellcheckBackend: options.ShellCheck.Backend}.Registrations(),
	}
}

func applicationResultView(result *application.Result) *Result {
	if result == nil {
		return nil
	}
	return &Result{RunnerEvidence: result.RunnerEvidence, Diagnostics: result.Diagnostics, Documents: result.Documents, Inputs: result.Inputs, Configurations: result.Configurations, FileCount: result.FileCount}
}

func backendProcessOptions(tool Tool) *processconfig.Options {
	if tool.Process != nil {
		return toolOptions(*tool.Process)
	}
	if tool.Backend != nil {
		return nil
	}
	return toolOptions(Process{})
}
