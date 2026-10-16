// Package application coordinates input selection, analysis and runtime ownership.
// It has no terminal, command parser, output destination or exit-code policy.
package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/analyzerhost"
	"actionlint.kjanat.dev/internal/engine"
	"actionlint.kjanat.dev/internal/fsread"
	"actionlint.kjanat.dev/internal/processconfig"
	"actionlint.kjanat.dev/internal/projectpath"
	"actionlint.kjanat.dev/internal/render"
	"actionlint.kjanat.dev/internal/repoconfig"
	"actionlint.kjanat.dev/internal/resolution"
	"actionlint.kjanat.dev/internal/workspace"
	"actionlint.kjanat.dev/resolver"
)

// Options selects configuration and analysis backends for one application session.
type Options struct {
	Resolver                       resolver.Resolver
	WorkingDir, ConfigFile         string
	ConfigOverlays                 []repoconfig.Overlay
	SkipProjectConfig              bool
	Analyzers                      []analyzer.Registration
	ReadFile                       func(string) ([]byte, error)
	ShellCheck, Ruff               string
	ShellcheckOptions, RuffOptions *processconfig.Options
	ShellcheckBackend, RuffBackend analyzer.ScriptBackend
	LoadScriptEngine               func(context.Context, string) (analyzer.ScriptBackend, error)
	Presets                        repoconfig.Presets
	IgnorePatterns                 []string
	LogWriter                      io.Writer
	Verbose, Debug, LogSelection   bool
	LegacyErrors                   bool
}

// InputKind distinguishes explicit empty file sets from repository discovery.
type InputKind uint8

const (
	Files InputKind = iota
	Repository
	Stdin
)

// Input describes where sources come from. AllowEmptyRepository implements the
// pre-commit convention; it still validates and records selected configuration.
type Input struct {
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
type Result struct {
	render.Result
	Inputs []string
}

// Check owns the complete runtime lifetime, including failed input selection and
// cancellation. A non-nil result with an error retains partial analysis findings.
func Check(ctx context.Context, options Options, input Input) (result *Result, err error) {
	if options.Resolver != nil {
		if factory, ok := options.Resolver.(resolver.SessionFactory); ok {
			options.Resolver = factory.NewSession()
		}
		options.Analyzers = append(slices.Clone(options.Analyzers), analyzer.Registration{Analyzer: resolution.Analyzer{Resolver: options.Resolver}})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	log := options.LogWriter
	if log == nil {
		log = io.Discard
	}

	engines := analyzerhost.NewScriptEngines(options.LoadScriptEngine)
	defer func() { err = errors.Join(err, engines.Close(context.WithoutCancel(ctx))) }()
	request := engine.AnalysisRequest{
		Analyzers: options.Analyzers, ReadFile: options.ReadFile, ShellCheck: options.ShellCheck, Ruff: options.Ruff,
		ShellcheckOptions: options.ShellcheckOptions, RuffOptions: options.RuffOptions,
		ShellcheckBackend: options.ShellcheckBackend, RuffBackend: options.RuffBackend, RulePresets: options.Presets,
		LogWriter: log,
	}
	if options.LoadScriptEngine != nil {
		request.LoadScriptEngine = engines.Load
	}
	if options.Verbose {
		request.LogLevel = engine.LogLevelVerbose
	} else if options.Debug {
		request.LogLevel = engine.LogLevelDebug
	}
	request.IgnorePatterns, err = repoconfig.CompileIgnorePatterns(options.IgnorePatterns)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, registration := range options.Analyzers {
		if registration.Analyzer != nil {
			names = append(names, registration.Analyzer.Descriptor().Name)
		}
	}
	selectionLog := func(args ...any) {
		if options.LogSelection && request.LogLevel >= engine.LogLevelVerbose {
			_, _ = io.WriteString(log, "verbose: "+fmt.Sprintln(args...))
		}
	}
	session, err := workspace.New(workspace.Options{Context: ctx, WorkingDir: options.WorkingDir, StdinFileName: input.Name,
		ConfigFile: options.ConfigFile, ConfigOverlays: options.ConfigOverlays, SkipProjectConfig: options.SkipProjectConfig,
		ReadFile: options.ReadFile, RuleNames: names, Log: selectionLog, LogWriter: log})
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
		var analysis *engine.AnalysisResult
		analysis, err = engine.Analyze(ctx, request)
		if analysis != nil {
			analysis.Configurations = selection.Configurations
			inputs := slices.Compact(slices.Sorted(slices.Values(append(analysis.Inputs, selection.Inputs...))))
			result = &Result{Result: analysis.RenderResult(), Inputs: inputs}
		}
	}
	if options.LegacyErrors && err != nil {
		err = engine.LegacyAnalysisError(err)
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
