// Package analysis checks workflow and action sources without selecting a CLI
// output format. Findings are results; an error indicates incomplete analysis.
package analysis

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/actions"
	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/config"
	configreport "actionlint.kjanat.dev/v2/config/report"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/document"
	"actionlint.kjanat.dev/v2/internal/application"
	configvalue "actionlint.kjanat.dev/v2/internal/config/value"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/tools/backend"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
	"actionlint.kjanat.dev/v2/internal/workspace"
	"actionlint.kjanat.dev/v2/repository"
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
// Process selects the built-in native adapter. A zero Tool disables native execution.
// Supplying both is an error. The caller owns backend lifetime and may reuse it.
type Tool struct {
	Backend analyzer.ScriptBackend
	Process *Process
}

// Options controls configuration, external tools and filesystem access.
// ReadFile must support concurrent calls. It does not sandbox external tools.
// A nil read error promises complete content, including when cancellation races.
type Options struct {
	// RemoteActions supplies manifests resolved to immutable commit identities.
	// Nil leaves remote metadata unavailable while reference syntax is checked.
	RemoteActions actions.Provider
	// Repository supplies host knowledge for each absolute project root.
	// Configured repository facts override it after inheritance and file selection.
	Repository func(context.Context, string) repository.Snapshot
	Runners    runner.Options
	Resolver   resolver.Resolver
	Analyzers  []analyzer.Registration
	// LoadScriptEngine authorizes configuration-selected Wasm engines. Nil disables
	// them. Calls receive resolved paths and may be concurrent. Each analysis loads
	// each path once and closes returned backends implementing Close(context.Context) error.
	// Explicit Tool selections take precedence.
	LoadScriptEngine func(context.Context, string) (analyzer.ScriptBackend, error)
	// WorkingDir roots source paths, config selection and local references.
	// Empty uses the process working directory; Analyze does not discover config.
	WorkingDir string
	ReadFile   func(string) ([]byte, error)
	// Tools selects how each bundled tool runs, keyed by tool id. The
	// function Tools lists the valid ids; any other key is an error. A
	// bundled tool missing from the map gets the zero Tool.
	Tools          map[string]Tool
	Presets        config.Presets
	IgnorePatterns []string
	// AllowConfigProcesses authorizes repository-selected executable invocations.
	AllowConfigProcesses bool
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
	Configurations []configreport.Report   `json:"configurations"`
	FileCount      int                     `json:"file_count"`
}

// Analyze checks already-loaded sources. It does not perform source discovery.
func Analyze(ctx context.Context, request Request) (*Result, error) {
	options, err := fileOptions(FileOptions{Options: request.Options})
	if err != nil {
		return nil, err
	}
	project := workspace.Identity(options.WorkingDir)
	sources := make([]workspace.Source, len(request.Sources))
	for i, source := range request.Sources {
		sources[i] = workspace.Source{Path: source.Path, Content: source.Content, Config: configvalue.Unwrap(source.Config), Project: project}
	}
	result, err := application.Analyze(ctx, options, sources)
	return applicationResultView(result), err
}

// Files checks explicit paths with project discovery and configuration loading.
func Files(ctx context.Context, paths []string, options FileOptions) (*Result, error) {
	settings, err := fileOptions(options)
	if err != nil {
		return nil, err
	}
	resolvedPaths := make([]string, len(paths))
	for i, path := range paths {
		resolvedPaths[i] = path
		if path != "<stdin>" && !filepath.IsAbs(path) {
			resolvedPaths[i] = filepath.Join(settings.WorkingDir, path)
		}
	}
	result, err := application.Check(ctx, settings, application.Input{Kind: application.Files, Paths: resolvedPaths})
	return applicationResultView(result), err
}

// Repository discovers and checks workflows and actions under a repository.
func Repository(ctx context.Context, directory string, options FileOptions) (*Result, error) {
	settings, err := fileOptions(options)
	if err != nil {
		return nil, err
	}
	if directory != "" && !filepath.IsAbs(directory) {
		directory = filepath.Join(settings.WorkingDir, directory)
	}
	result, err := application.Check(ctx, settings, application.Input{Kind: application.Repository, Directory: directory})
	return applicationResultView(result), err
}

// Tools returns the ids of the tools bundled with actionlint, the keys
// Options.Tools accepts.
func Tools() []string {
	return registry.IDs()
}

func toolOptions(tool Process) *processconfig.Options {
	return &processconfig.Options{Executable: &tool.Executable, LauncherArguments: tool.Arguments, Environment: tool.Environment, WorkingDir: tool.WorkingDir}
}

func fileOptions(options FileOptions) (application.Options, error) {
	root, err := filepath.Abs(options.WorkingDir)
	if err != nil {
		return application.Options{}, err
	}
	if options.ConfigFile != "" && !filepath.IsAbs(options.ConfigFile) {
		options.ConfigFile = filepath.Join(root, options.ConfigFile)
	}
	managed := map[string]bool{}
	for _, registration := range options.Analyzers {
		if integrator, ok := registration.Analyzer.(analyzer.ToolIntegrator); ok {
			if descriptor := integrator.ToolIntegration(); descriptor != nil {
				managed[descriptor.Tool] = true
			}
		}
	}
	ids := Tools()
	for _, id := range slices.Sorted(maps.Keys(options.Tools)) {
		if !slices.Contains(ids, id) {
			return application.Options{}, fmt.Errorf("analysis: unknown tool %q; bundled tools: %s", id, strings.Join(ids, ", "))
		}
	}
	selections := registry.Selections{}
	for _, name := range ids {
		tool := options.Tools[name]
		// A managed analyzer replaces an unselected tool without a native fallback.
		if managed[name] && tool.Process == nil && tool.Backend == nil {
			selections[name] = backend.Selection{Command: name, ManagedOnly: true}
			continue
		}
		selections[name] = backend.Selection{
			Command: configuredToolName(name, tool, options.LoadScriptEngine != nil),
			Process: backendProcessOptions(tool, options.LoadScriptEngine != nil),
			Backend: tool.Backend,
		}
	}
	return application.Options{
		Repository:    options.Repository,
		RemoteActions: options.RemoteActions,
		Resolver:      options.Resolver, Analyzers: options.Analyzers, Runners: options.Runners, WorkingDir: root, ConfigFile: options.ConfigFile,
		ConfigOverlays: options.ConfigOverlays, SkipProjectConfig: options.SkipProjectConfig,
		ReadFile:             options.ReadFile,
		AllowConfigProcesses: options.AllowConfigProcesses,
		LoadScriptEngine:     options.LoadScriptEngine,

		Presets:        options.Presets,
		IgnorePatterns: options.IgnorePatterns, Tools: registry.Registrations(selections),
	}, nil
}

func applicationResultView(result *application.Result) *Result {
	if result == nil {
		return nil
	}
	return &Result{RunnerEvidence: result.RunnerEvidence, Diagnostics: result.Diagnostics, Documents: result.Documents, Inputs: result.Inputs, Configurations: result.Configurations, FileCount: result.FileCount}
}

func backendProcessOptions(tool Tool, configured bool) *processconfig.Options {
	if tool.Process != nil {
		return toolOptions(*tool.Process)
	}
	if tool.Backend != nil {
		return nil
	}
	options := toolOptions(Process{})
	options.AllowWasm = configured
	return options
}

func configuredToolName(name string, tool Tool, configured bool) string {
	if configured && tool.Backend == nil && tool.Process == nil {
		return name
	}
	return ""
}
