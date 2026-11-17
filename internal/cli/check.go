package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"actionlint.kjanat.dev/v2/internal/cli/arguments"
	"actionlint.kjanat.dev/v2/internal/cli/commandio"
	"actionlint.kjanat.dev/v2/internal/input/checkrequest"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/cli/exitcode"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/files"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
	"actionlint.kjanat.dev/v2/internal/model/availability"
	"actionlint.kjanat.dev/v2/internal/output"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/edits"
	"actionlint.kjanat.dev/v2/internal/results/policy"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

func executeCheck(ctx context.Context, streams Command, inv checkRequest) (status int, err error) {
	c, r := inv.Check, inv.Render
	ignoreSelectors, err := policy.CompileIgnoreSelectors(c.IgnoreSelectors)
	if err != nil {
		return 0, arguments.UsageError{Cause: err}
	}
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
	if destination.file != nil && r.Color == output.ColorAuto {
		r.Color = output.ColorNever
	}
	log := streams.Stderr
	if inv.JSON {
		log = &commandJSONLogWriter{out: log}
	}
	log = &synchronizedLogWriter{out: log}
	writeCheckBuildIdentity(log, c, r)
	for _, warning := range c.Deprecations {
		_, _ = fmt.Fprintln(log, "warning: "+warning)
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return 0, err
	}
	options, err := c.checkingOptions(workingDir, streams.Acquisition, log, inv.Legacy)
	if err != nil {
		return 0, err
	}
	options.NoIgnore = c.NoIgnore
	options.IgnorePatterns, options.IgnoreSelectors = c.IgnoreRegex, ignoreSelectors
	options.Verbose, options.Debug = c.Verbose && !r.Quiet, c.Debug && !r.Quiet
	options.LogSelection, options.LegacyErrors = inv.Legacy, inv.Legacy
	options.Presets = configuration.Presets{Strict: c.Strict}
	if c.ExperimentalSet {
		options.Presets.Experimental = &c.Experimental
	}
	renderers, err := newResultRenderers(r)
	if err != nil {
		return 0, err
	}
	input := checkrequest.Request{Kind: checkrequest.Files, Paths: c.Paths}
	if len(c.Paths) == 0 {
		input.Kind, input.AllowEmptyRepository = checkrequest.Repository, c.PreCommit
	} else if len(c.Paths) == 1 && c.Paths[0] == "-" {
		input.Kind, input.Reader, input.Name, input.NormalizePath = checkrequest.Stdin, streams.Stdin, c.StdinFilename, !inv.Legacy
	}
	result, err := checking.CheckProjects(ctx, options, input, c.pluginSelection(streams.Acquisition))
	if err != nil {
		if _, ok := errors.AsType[*pluginexecution.LoadError](err); ok && len(c.Plugins) != 0 {
			return 0, arguments.UsageError{Cause: err}
		}
		if _, ok := errors.AsType[*checking.UnknownToolError](err); ok {
			return 0, arguments.UsageError{Cause: err}
		}
		if result != nil {
			err = &checkAnalysisError{result: result, err: err}
		}
		return 0, err
	}
	inputs := append([]string{c.Config.Path, r.TemplateFile, c.RunnerSnapshot}, result.Inputs...)
	for _, source := range c.Plugins {
		_, local, err := artifacts.NormalizeSource(source, workingDir)
		if err != nil {
			return 0, arguments.UsageError{Cause: err}
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
	for _, item := range result.ActionMetadata {
		reason := item.Availability.Reason()
		if c.Offline && (reason == availability.OfflineReason || reason == availability.CacheMissReason) {
			network.Warn(ctx, "Skipping unavailable metadata for "+item.Reference+" because it is not cached. Run actionlint cache warm while online.")
		}
		if c.WarmCache && item.Availability.State() != "available" {
			return 0, &checkAnalysisError{result: result, err: fmt.Errorf("could not load metadata for %s: %s", item.Reference, reason)}
		}
	}
	if c.WarmCache {
		if err := network.CacheError(ctx); err != nil {
			return 0, &checkAnalysisError{result: result, err: err}
		}
		if inv.JSON {
			return 0, commandio.WriteJSON(out, struct {
				CacheDirectory string `json:"cache_directory"`
			}{network.Settings(ctx).CacheDirectory})
		}
		_, err := fmt.Fprintln(out, "Dependencies cached in "+network.Settings(ctx).CacheDirectory)
		return 0, err
	}
	if c.Fix {
		if input.Kind == checkrequest.Stdin {
			return 0, arguments.UsageError{Cause: errors.New("fix requires files; stdin is read-only")}
		}
		reader := fixTargetReader(ctx, result.Inputs)
		plan, planErr := edits.Build(ctx, results.Diagnostics(result.Diagnostics), reader, c.IncludeSuggestedFixes)
		if planErr != nil {
			return 0, planErr
		}
		if !c.ApplyFixes {
			return 0, commandio.WriteJSON(out, plan)
		}
		written, applyErr := output.ApplyEdits(ctx, plan, reader)
		if applyErr != nil {
			return 0, fmt.Errorf("fix stopped after writing %d files: %w", len(written), applyErr)
		}
		result, err = checking.CheckProjects(ctx, options, input, c.pluginSelection(streams.Acquisition))
		if err != nil {
			return 0, err
		}
	}
	presentation := output.FromAnalysis(*result)
	presentation.CheckResult = exitcode.AnalysisResult(*result)
	if err := renderCheckResult(ctx, out, r, inv.Legacy, renderers, presentation); err != nil {
		return 0, err
	}
	if inv.Legacy && options.Verbose && result.FileCount > 1 {
		_, _ = fmt.Fprintf(log, "verbose: Found %d errors in %d files\n", len(result.Diagnostics), result.FileCount)
	}
	status = presentation.ExitCode
	if r.Summary && !r.Quiet {
		summary := report.NewCheckSummary(*result)
		if inv.JSON {
			err = commandio.WriteJSON(streams.Stderr, map[string]report.CheckSummary{"summary": summary})
		} else {
			err = writeCheckSummary(streams.Stderr, inv.Render.Color, summary)
		}
	}
	return status, err
}

func fixTargetReader(ctx context.Context, inputs []string) *files.Reader {
	allowed := newCommandFiles(inputs)
	disk := files.New(ctx, nil, nil)
	return files.New(ctx, func(path string) ([]byte, error) {
		if !allowed.contains(path) {
			return nil, fmt.Errorf("fix target %s was not an analysis input", path)
		}
		return disk.ReadFile(path)
	}, nil)
}

func writeCheckBuildIdentity(log io.Writer, c checkInvocation, r renderOptions) {
	if (c.Verbose || c.Debug) && !r.Quiet {
		build := commandBuild()
		_, _ = fmt.Fprintf(log,
			"verbose: actionlint version: %s\n"+
				"verbose: installed: %s\n"+
				"verbose: Go version: %s\n"+
				"verbose: operating system: %s\n"+
				"verbose: architecture: %s\n",
			build.Version, build.InstalledFrom, build.GoVersion, build.OS, build.GOARCH)
	}
}

// checkAnalysisError carries collected findings to the existing failure report.
type checkAnalysisError struct {
	result *report.Analysis
	err    error
}

func (e *checkAnalysisError) Error() string { return e.err.Error() }
func (e *checkAnalysisError) Unwrap() error { return e.err }
