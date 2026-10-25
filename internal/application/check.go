// Package application coordinates input selection, analysis and runtime ownership.
// It has no terminal, command parser, output destination or exit-code policy.
package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"slices"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/projectpath"
	"actionlint.kjanat.dev/v2/internal/report"
	"actionlint.kjanat.dev/v2/internal/resolution"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/internal/workspace"
	"actionlint.kjanat.dev/v2/resolver"
	"actionlint.kjanat.dev/v2/runner"
)

// Options selects configuration and analysis backends for one application session.
type Options struct {
	Runners                      runner.Options
	Tools                        []tools.Registration
	Resolver                     resolver.Resolver
	WorkingDir, ConfigFile       string
	ConfigOverlays               []repoconfig.Overlay
	SkipProjectConfig            bool
	Analyzers                    []analyzer.Registration
	ReadFile                     func(string) ([]byte, error)
	OnConfigLoaded               func(repoconfig.Report)
	OnFilesSelected              func([]string)
	LoadScriptEngine             func(context.Context, string) (analyzer.ScriptBackend, error)
	Presets                      repoconfig.Presets
	IgnorePatterns               []string
	LogWriter                    io.Writer
	Verbose, Debug, LogSelection bool
	LegacyErrors                 bool
	// AllowConfigProcesses authorizes repository-selected executable invocations.
	AllowConfigProcesses bool
}

// InputKind distinguishes explicit empty file sets from repository discovery.
type InputKind uint8

const (
	Files InputKind = iota
	Repository
	Stdin
	prepared
)

// Input describes where sources come from. AllowEmptyRepository implements the
// pre-commit convention; it still validates and records selected configuration.
type Input struct {
	sources              []workspace.Source
	Kind                 InputKind
	Paths                []string
	Directory            string
	Reader               io.Reader
	Name                 string
	NormalizePath        bool
	AllowEmptyRepository bool
}

// Result is independent of the engine's traversal and mutable session state.
// Rendering and application-specific exit policies consume the same findings.
type Result = report.Analysis

// Check owns the complete runtime lifetime, including failed input selection and
// cancellation. A non-nil result with an error retains partial analysis findings.
func Check(ctx context.Context, options Options, input Input) (result *Result, err error) {
	options, err = ConfigureToolIntegrations(options)
	if err != nil {
		return nil, err
	}
	if options.Resolver != nil {
		if factory, ok := options.Resolver.(resolver.SessionFactory); ok {
			options.Resolver = factory.NewSession()
		}
		options.Analyzers = append(slices.Clone(options.Analyzers), analyzer.Registration{Analyzer: &resolution.Analyzer{Resolver: options.Resolver}})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	log := options.LogWriter
	if log == nil {
		log = io.Discard
	}

	processes := analyzerhost.NewProcessPool(ctx, runtime.NumCPU())
	engines := analyzerhost.NewScriptEngines(options.LoadScriptEngine)
	defer func() { processes.Wait(); err = errors.Join(err, engines.Close(context.WithoutCancel(ctx))) }()
	request := analysisRequest{Processes: processes, Runners: options.Runners, Analyzers: options.Analyzers, ReadFile: options.ReadFile, Tools: options.Tools, RulePresets: options.Presets, LogWriter: log, AllowConfigProcesses: options.AllowConfigProcesses}

	if options.LoadScriptEngine != nil {
		request.LoadScriptEngine = engines.Load
	}
	if options.Verbose {
		request.LogLevel = LogLevelVerbose
	} else if options.Debug {
		request.LogLevel = LogLevelDebug
	}
	request.IgnorePatterns, err = repoconfig.CompileIgnorePatterns(options.IgnorePatterns)
	if err != nil {
		return nil, err
	}
	if input.Kind == prepared {
		request.Sources, request.WorkingDir = input.sources, options.WorkingDir
		return analyzePrepared(ctx, request)
	}
	var names []string
	for _, registration := range options.Analyzers {
		if registration.Analyzer != nil {
			names = append(names, registration.Analyzer.Descriptor().Name)
		}
	}
	for _, registration := range options.Tools {
		names = append(names, registration.Descriptor.Name)
	}
	selectionLog := func(args ...any) {
		if options.LogSelection && request.LogLevel >= LogLevelVerbose {
			_, _ = io.WriteString(log, "verbose: "+fmt.Sprintln(args...))
		}
	}
	session, err := workspace.New(workspace.Options{
		Context: ctx, WorkingDir: options.WorkingDir, StdinFileName: input.Name,
		ConfigFile: options.ConfigFile, ConfigOverlays: options.ConfigOverlays, OnConfigLoaded: options.OnConfigLoaded, OnFilesSelected: options.OnFilesSelected, SkipProjectConfig: options.SkipProjectConfig,
		ReadFile: options.ReadFile, RuleNames: names, Log: selectionLog, LogWriter: log,
	})
	if err != nil {
		return nil, err
	}
	var selection *workspace.Selection
	switch input.Kind {
	case Files:
		selection, err = session.Files(input.Paths, nil)
	case Stdin:
		if input.Reader == nil {
			return nil, errors.New("stdin source requires a reader")
		}
		selection, err = session.ReadStdin(input.Reader, input.NormalizePath)
	case Repository:
		var listed []string
		empty := false
		if input.AllowEmptyRepository {
			empty, listed, err = emptyRepository(ctx, options.WorkingDir)
			if err != nil {
				return nil, err
			}
		}
		if empty {
			if options.Verbose {
				_, _ = fmt.Fprintf(log, "verbose: No workflow files to check from %q\n", options.WorkingDir)
			}
			selection, err = session.Empty()
		} else {
			selection, err = session.Repository(input.Directory)
		}
		if selection != nil {
			selection.Inputs = slices.Compact(slices.Sorted(slices.Values(append(selection.Inputs, listed...))))
		}
	default:
		return nil, fmt.Errorf("unknown input kind %d", input.Kind)
	}
	if err == nil {
		request.Sources = selection.Sources
		request.WorkingDir = session.WorkingDir()
		result, err = analyzePrepared(ctx, request)
		if result != nil {
			result.Configurations = selection.Configurations
			result.Inputs = slices.Compact(slices.Sorted(slices.Values(append(result.Inputs, selection.Inputs...))))
		}
	}
	if options.LegacyErrors && err != nil {
		err = LegacyAnalysisError(err)
	}
	return result, err
}

func emptyRepository(ctx context.Context, directory string) (bool, []string, error) {
	var listed []string
	reader := fsread.New(ctx, nil, func(path string) { listed = append(listed, path) })
	hasWorkflows := false
	var err error
	if root := projectpath.RepositoryRoot(directory); root != "" {
		hasWorkflows, err = projectpath.HasWorkflowFiles(root, reader)
		if err != nil {
			return false, nil, err
		}
	}
	typo, _ := projectpath.WorkflowTypo(directory, reader)
	return typo == "" && !hasWorkflows, listed, nil
}

// Analyze checks supplied snapshots through the same runtime ownership path as Check.
// It does not discover configuration or read the supplied source paths.
func Analyze(ctx context.Context, options Options, sources []workspace.Source) (*Result, error) {
	return Check(ctx, options, Input{Kind: prepared, sources: sources})
}

func analyzePrepared(ctx context.Context, request analysisRequest) (*Result, error) {
	analyzed, err := runAnalysis(ctx, request)
	if analyzed == nil {
		return nil, err
	}
	return &analyzed.Analysis, err
}
