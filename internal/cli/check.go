package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	commandargs "actionlint.kjanat.dev/v2/internal/cli/arguments"
	commandstreams "actionlint.kjanat.dev/v2/internal/cli/streams"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"

	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"

	"actionlint.kjanat.dev/v2/internal/checking"
	commandstatus "actionlint.kjanat.dev/v2/internal/cli/exitcode"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/files"
	resolverprocess "actionlint.kjanat.dev/v2/internal/input/references/process"
	render "actionlint.kjanat.dev/v2/internal/output"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	pluginexec "actionlint.kjanat.dev/v2/internal/plugins/execution"
	"actionlint.kjanat.dev/v2/internal/results/edits"
	resultreport "actionlint.kjanat.dev/v2/internal/results/report"
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
	log = &synchronizedLogWriter{out: log}
	for _, warning := range c.Deprecations {
		_, _ = fmt.Fprintln(log, "warning: "+warning)
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return 0, err
	}
	options := checking.Options{
		NoIgnore:             c.NoIgnore,
		AllowConfigProcesses: c.AllowConfigProcesses,
		WorkingDir:           workingDir, ConfigFile: c.Config.Path,
		SkipProjectConfig: c.Config.Disabled || (!inv.Legacy && c.Config.Path != ""),

		IgnorePatterns: c.IgnoreRegex, Verbose: c.Verbose && !r.Quiet,
		Debug: c.Debug && !r.Quiet, LogWriter: log, LogSelection: inv.Legacy,
		LegacyErrors: inv.Legacy, Presets: repoconfig.Presets{Strict: c.Strict}, Tools: registry.Registrations(c.Tools),
	}
	if c.ResolverCommand != "" {
		options.Resolver = resolverprocess.NewProcess(c.ResolverCommand, c.ResolverArguments, 0)
	}
	options.RemoteActions, err = c.actionMetadata()
	if err != nil {
		return 0, err
	}
	options.Runners, err = c.runners()
	if err != nil {
		return 0, err
	}
	if c.ExperimentalSet {
		options.Presets.Experimental = &c.Experimental
	}
	renderers, err := newResultRenderers(r)
	if err != nil {
		return 0, err
	}
	input := inputmodel.Request{Kind: inputmodel.Files, Paths: c.Paths}
	if len(c.Paths) == 0 {
		input.Kind, input.AllowEmptyRepository = inputmodel.Repository, c.PreCommit
	} else if len(c.Paths) == 1 && c.Paths[0] == "-" {
		input.Kind, input.Reader, input.Name, input.NormalizePath = inputmodel.Stdin, streams.Stdin, c.StdinFilename, !inv.Legacy
	}
	if len(c.Plugins) != 0 && c.PluginLock != "" {
		return 0, commandargs.UsageError{Cause: errors.New("--plugin and --plugin-lock cannot be combined")}
	}
	result, err := checking.CheckProjects(ctx, options, input, c.pluginSelection())
	if err != nil {
		if _, ok := errors.AsType[*pluginexec.LoadError](err); ok && len(c.Plugins) != 0 {
			return 0, commandargs.UsageError{Cause: err}
		}
		if _, ok := errors.AsType[*checking.UnknownToolError](err); ok {
			return 0, commandargs.UsageError{Cause: err}
		}
		if result != nil {
			err = &checkAnalysisError{result: result, err: err}
		}
		return 0, err
	}
	inputs := append([]string{c.Config.Path, r.TemplateFile, c.RunnerSnapshot}, result.Inputs...)
	for _, source := range c.Plugins {
		_, local, err := pluginartifact.NormalizeSource(source, workingDir)
		if err != nil {
			return 0, commandargs.UsageError{Cause: err}
		}
		if local != "" {
			inputs = append(inputs, local)
		}
	}
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
	if result.FileCount == 0 && !c.PreCommit {
		return 0, errors.New("no workflow or action files were selected; check files.includes and files.excludes")
	}
	if c.Fix {
		if input.Kind == inputmodel.Stdin {
			return 0, commandargs.UsageError{Cause: errors.New("fix requires files; stdin is read-only")}
		}
		allowed := map[string]bool{}
		for _, path := range result.Inputs {
			absolute, e := filepath.Abs(path)
			if e != nil {
				return 0, e
			}
			allowed[absolute] = true
		}
		disk := files.New(ctx, nil, nil)
		reader := files.New(ctx, func(path string) ([]byte, error) {
			if !allowed[path] {
				return nil, fmt.Errorf("fix target %s was not an analysis input", path)
			}
			return disk.ReadFile(path)
		}, nil)
		plan, planErr := edits.Build(ctx, result.Diagnostics, reader, c.IncludeSuggestedFixes)
		if planErr != nil {
			return 0, planErr
		}
		if !c.ApplyFixes {
			return 0, commandstreams.WriteJSON(out, plan)
		}
		written, applyErr := render.ApplyEdits(ctx, plan, reader)
		if applyErr != nil {
			return 0, fmt.Errorf("fix stopped after writing %d files: %w", len(written), applyErr)
		}
		result, err = checking.CheckProjects(ctx, options, input, c.pluginSelection())
		if err != nil {
			return 0, err
		}
	}
	presentation := render.FromAnalysis(*result)
	presentation.CheckResult = commandstatus.AnalysisResult(*result)
	if err := renderCheckResult(ctx, out, r, inv.Legacy, renderers, presentation); err != nil {
		return 0, err
	}
	if inv.Legacy && options.Verbose && result.FileCount > 1 {
		_, _ = fmt.Fprintf(log, "verbose: Found %d errors in %d files\n", len(result.Diagnostics), result.FileCount)
	}
	status = presentation.ExitCode
	if r.Summary && !r.Quiet {
		summary := resultreport.NewCheckSummary(result.FileCount, result.Diagnostics)
		if inv.JSON {
			err = commandstreams.WriteJSON(streams.Stderr, map[string]resultreport.CheckSummary{"summary": summary})
		} else {
			err = writeCheckSummary(streams.Stderr, inv.Render.Color, summary)
		}
	}
	return status, err
}

// checkAnalysisError carries collected findings to the existing failure report.
type checkAnalysisError struct {
	result *checking.Result
	err    error
}

func (e *checkAnalysisError) Error() string { return e.err.Error() }
func (e *checkAnalysisError) Unwrap() error { return e.err }
