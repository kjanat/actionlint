package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"actionlint.kjanat.dev/internal/tools/adapters"

	"actionlint.kjanat.dev/internal/application"
	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/fix"
	"actionlint.kjanat.dev/internal/fsread"
	"actionlint.kjanat.dev/internal/localapp"
	"actionlint.kjanat.dev/internal/render"
	"actionlint.kjanat.dev/internal/resolution"
)

func executeCheck(ctx context.Context, streams Command, inv checkRequest) (status int, err error) {
	c, r := inv.Check, inv.Render
	overwrite, err := selectReportDestination(ctx, streams, r.OutputFile, inv.Yes, slices.Contains(c.Paths, "-"))
	if err != nil {
		return 0, err
	}
	r, err = resolveTemplate(r)
	if err != nil {
		return 0, err
	}

	destination, err := openReportDestination(streams.Stdout, r.OutputFile, overwrite)
	if err != nil {
		return 0, err
	}
	defer func() { err = destination.finish(err) }()
	out := destination.writer
	if destination.file != nil && r.Color == render.ColorAuto {
		r.Color = render.ColorNever
	}
	log := streams.Stderr
	if inv.JSON {
		log = &commandJSONLogWriter{out: log}
	}
	if c.Pyflakes != "" {
		fmt.Fprintln(log, "warning: --pyflakes is deprecated; use --ruff for Python script linting")
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return 0, err
	}
	options := application.Options{
		WorkingDir: workingDir, ConfigFile: c.Config.Path,
		SkipProjectConfig: c.Config.Disabled || (!inv.Legacy && c.Config.Path != ""),

		IgnorePatterns: c.IgnoreRegex, Verbose: c.Verbose && !r.Quiet,
		Debug: c.Debug && !r.Quiet, LogWriter: log, LogSelection: inv.Legacy,
		LegacyErrors: inv.Legacy, Presets: repoconfig.Presets{Strict: c.Strict}, Tools: adapters.Options{ShellCheck: c.ShellCheck, ShellcheckOptions: c.ShellcheckOptions,
			Ruff: c.Ruff, RuffOptions: c.RuffOptions}.Registrations(),
	}
	if c.ResolverCommand != "" {
		options.Resolver = &resolution.Process{Executable: c.ResolverCommand, Arguments: c.ResolverArguments}
	}
	if c.ExperimentalSet {
		options.Presets.Experimental = &c.Experimental
	}
	var renderer *render.Renderer
	if inv.Legacy {
		renderer, err = newResultRenderer(r)
		if err != nil {
			return 0, err
		}
	}
	input := application.Input{Kind: application.Files, Paths: c.Paths}
	if len(c.Paths) == 0 {
		input.Kind, input.AllowEmptyRepository = application.Repository, c.PreCommit
	} else if len(c.Paths) == 1 && c.Paths[0] == "-" {
		input.Kind, input.Reader, input.Name, input.NormalizePath = application.Stdin, streams.Stdin, c.StdinFilename, !inv.Legacy
	}
	result, err := localapp.Check(ctx, options, input, c.Plugins)
	if err != nil {
		if _, ok := errors.AsType[*localapp.PluginLoadError](err); ok {
			return 0, commandUsageError{err}
		}
		if result != nil {
			err = &checkAnalysisError{result: result, err: err}
		}
		return 0, err
	}
	inputs := append([]string{c.Config.Path, r.TemplateFile}, result.Inputs...)
	inputs = append(inputs, c.Paths...)
	if len(c.Paths) == 1 && c.Paths[0] == "-" {
		inputs = append(inputs, c.StdinFilename)
	}
	if r.OutputFile != "" && r.OutputFile != "-" {
		for _, path := range inputs {
			if path != "" && sameCommandFile(path, r.OutputFile) {
				return 0, fmt.Errorf("output file %q is also an input", r.OutputFile)
			}
		}
	}
	if c.Fix {
		if input.Kind == application.Stdin {
			return 0, commandUsageError{errors.New("fix requires files; stdin is read-only")}
		}
		allowed := map[string]bool{}
		for _, path := range result.Inputs {
			absolute, e := filepath.Abs(path)
			if e != nil {
				return 0, e
			}
			allowed[absolute] = true
		}
		disk := fsread.New(ctx, nil, nil)
		reader := fsread.New(ctx, func(path string) ([]byte, error) {
			if !allowed[path] {
				return nil, fmt.Errorf("fix target %s was not an analysis input", path)
			}
			return disk.ReadFile(path)
		}, nil)
		plan, planErr := fix.Build(ctx, result.Diagnostics, reader, c.IncludeSuggestedFixes)
		if planErr != nil {
			return 0, planErr
		}
		if !c.ApplyFixes {
			return 0, writeCommandJSON(out, plan)
		}
		written, applyErr := fix.Apply(ctx, plan, reader)
		if applyErr != nil {
			return 0, fmt.Errorf("fix stopped after writing %d files: %w", len(written), applyErr)
		}
		result, err = localapp.Check(ctx, options, input, c.Plugins)
		if err != nil {
			return 0, err
		}
	}
	if err := renderCheckResult(ctx, out, r, inv.Legacy, renderer, render.Prepare(*result)); err != nil {
		return 0, err
	}
	if inv.Legacy && options.Verbose && result.FileCount > 1 {
		_, _ = fmt.Fprintf(log, "verbose: Found %d errors in %d files\n", len(result.Diagnostics), result.FileCount)
	}
	if len(result.Diagnostics) > 0 {
		status = render.ExitSuccessProblemFound
	}
	if r.Summary && !r.Quiet {
		summary := render.CheckSummary{Files: result.FileCount, Findings: len(result.Diagnostics)}
		if inv.JSON {
			err = writeCommandJSON(streams.Stderr, map[string]render.CheckSummary{"summary": summary})
		} else {
			err = writeCheckSummary(streams.Stderr, inv.Render.Color, summary)
		}
	}
	return status, err
}

// checkAnalysisError carries collected findings to the existing failure report.
type checkAnalysisError struct {
	result *application.Result
	err    error
}

func (e *checkAnalysisError) Error() string { return e.err.Error() }
func (e *checkAnalysisError) Unwrap() error { return e.err }
