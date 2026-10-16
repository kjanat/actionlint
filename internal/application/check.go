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
	"actionlint.kjanat.dev/internal/engine"
	"actionlint.kjanat.dev/internal/fsread"
	"actionlint.kjanat.dev/internal/processconfig"
	"actionlint.kjanat.dev/internal/projectpath"
	"actionlint.kjanat.dev/internal/render"
	"actionlint.kjanat.dev/internal/repoconfig"
)

// Options selects configuration and analysis backends for one application session.
type Options struct {
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	log := options.LogWriter
	if log == nil {
		log = io.Discard
	}
	session, err := engine.NewAnalysisSession(engine.AnalysisOptions{
		Context: ctx, WorkingDir: options.WorkingDir, StdinFileName: input.Name,
		ConfigFile: options.ConfigFile, ConfigOverlays: options.ConfigOverlays,
		SkipProjectConfig: options.SkipProjectConfig, ReadFile: options.ReadFile,
		Analyzers:        options.Analyzers,
		LoadScriptEngine: options.LoadScriptEngine,
		Shellcheck:       options.ShellCheck, Ruff: options.Ruff,
		ShellcheckOptions: options.ShellcheckOptions, RuffOptions: options.RuffOptions,
		ShellcheckBackend: options.ShellcheckBackend, RuffBackend: options.RuffBackend,
		RulePresets: options.Presets, IgnorePatterns: options.IgnorePatterns,
		Verbose: options.Verbose, Debug: options.Debug, LogWriter: log,
		QuietSelection: !options.LogSelection,
	})
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, session.Close(context.WithoutCancel(ctx))) }()
	var analysis *engine.AnalysisResult
	switch input.Kind {
	case Files:
		analysis, err = session.Files(input.Paths, nil)
	case Stdin:
		if input.Reader == nil {
			return nil, errors.New("stdin source requires a reader")
		}
		analysis, err = session.ReadStdin(input.Reader, input.NormalizePath)
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
			analysis, err = session.Empty()
		} else {
			analysis, err = session.Repository(input.Directory)
		}
		if analysis != nil {
			analysis.Inputs = slices.Compact(slices.Sorted(slices.Values(append(analysis.Inputs, listed...))))
		}
	default:
		return nil, fmt.Errorf("unknown input kind %d", input.Kind)
	}
	if analysis != nil {
		result = &Result{Result: analysis.RenderResult(), Inputs: analysis.Inputs}
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
