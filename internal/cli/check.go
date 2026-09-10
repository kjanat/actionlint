package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"actionlint.kjanat.dev"
	"github.com/fatih/color"
	"github.com/mattn/go-colorable"
)

func executeCheck(ctx context.Context, streams Command, inv invocation) (status int, err error) {
	c, r := inv.Check, inv.Render
	r, err = resolveTemplate(r)
	if err != nil {
		return 0, err
	}

	// A completed analysis replaces the previous report atomically.
	out := streams.Stdout
	if r.OutputFile != "" && r.OutputFile != "-" {
		file, e := os.CreateTemp(filepath.Dir(r.OutputFile), ".actionlint-report-*")
		if e != nil {
			return 0, e
		}
		if info, statErr := os.Stat(r.OutputFile); statErr == nil {
			if modeErr := file.Chmod(info.Mode().Perm()); modeErr != nil {
				file.Close()
				os.Remove(file.Name())
				return 0, modeErr
			}
		}
		out = file
		defer func() {
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
			if err == nil {
				err = os.Rename(file.Name(), r.OutputFile)
			}
			_ = os.Remove(file.Name())
		}()
		if r.Color == actionlint.ColorOptionKindAuto {
			r.Color = actionlint.ColorOptionKindNever
		}
	}
	r.resolveGitHubActionsColor(out)
	log := streams.Stderr
	if inv.JSON {
		log = &commandJSONLogWriter{out: log}
	}
	options := actionlint.LinterOptions{
		Context: ctx, LogWriter: log, Color: r.Color, Oneline: r.Oneline,
		Shellcheck: c.ShellCheck, Pyflakes: c.Pyflakes, ConfigFile: c.Config.Path,
		IgnorePatterns: c.IgnoreRegex, StdinFileName: c.StdinFilename,
		Format: r.Template, OutputFormat: r.Format,
		Verbose: c.Verbose && !r.Quiet, Debug: c.Debug && !r.Quiet,
	}
	if inv.Legacy {
		out = actionlint.LegacyColorOutput(out, r.Color)
	} else {
		options.WorkingDir, err = os.Getwd()
		if err != nil {
			return 0, err
		}
	}
	app, err := actionlint.NewAnalysisSession(actionlint.AnalysisOptions{
		Context: ctx, WorkingDir: options.WorkingDir, StdinFileName: options.StdinFileName,
		ConfigFile: options.ConfigFile, Shellcheck: options.Shellcheck, Pyflakes: options.Pyflakes,
		IgnorePatterns: options.IgnorePatterns, Verbose: options.Verbose, Debug: options.Debug, LogWriter: log,
		SkipProjectConfig: c.Config.Disabled || (!inv.Legacy && c.Config.Path != ""), QuietSelection: !inv.Legacy,
	})
	if err != nil {
		return 0, err
	}
	var renderer *actionlint.AnalysisRenderer
	if inv.Legacy {
		// Root invocations validate templates before reading any workflow input.
		renderer, err = actionlint.NewAnalysisRenderer(r.Format, r.Template, r.Oneline)
		if err != nil {
			return 0, err
		}
		if options.Debug && !options.Verbose {
			_, _ = fmt.Fprintf(log, "[Linter] Create a Linter instance with option %#v\n", &options)
		}
	}
	result, err := analyzeCommand(app, streams.Stdin, c.Paths, !inv.Legacy)
	if err != nil {
		if inv.Legacy {
			err = actionlint.LegacyAnalysisError(err)
		}
		return 0, err
	}
	inputs := append([]string{c.Config.Path, r.TemplateFile, c.StdinFilename}, result.Inputs...)
	if r.OutputFile != "" && r.OutputFile != "-" {
		for _, path := range inputs {
			if path != "" && sameCommandFile(path, r.OutputFile) {
				return 0, fmt.Errorf("output file %q is also an input", r.OutputFile)
			}
		}
	}
	if !inv.Legacy {
		renderer, err = actionlint.NewAnalysisRenderer(r.Format, r.Template, r.Oneline)
		if err != nil {
			return 0, err
		}
		previous := color.NoColor
		defer func() { color.NoColor = previous }()
		switch r.Color {
		case actionlint.ColorOptionKindNever:
			color.NoColor = true
		case actionlint.ColorOptionKindAlways:
			color.NoColor = false
		}
		if file, ok := out.(*os.File); ok && !color.NoColor {
			out = colorable.NewColorable(file)
		}
	}
	var writes *commandResultWriter
	if !inv.Legacy || r.OutputFile != "" {
		writes = &commandResultWriter{Writer: out}
		out = writes
	}
	if err := renderer.Render(out, result); err != nil {
		return 0, err
	}
	if writes != nil && writes.err != nil {
		return 0, writes.err
	}
	app.Completed(result)
	if len(result.Diagnostics) > 0 {
		status = actionlint.ExitStatusSuccessProblemFound
	}
	if r.Summary && !r.Quiet {
		summary := actionlint.CheckSummary{Files: result.FileCount(), Findings: len(result.Diagnostics)}
		if inv.JSON {
			err = writeCommandJSON(streams.Stderr, map[string]actionlint.CheckSummary{"summary": summary})
		} else {
			_, err = fmt.Fprintf(streams.Stderr, "Checked %d workflows; %d findings.\n", summary.Files, summary.Findings)
		}
	}
	return status, err
}

func analyzeCommand(app *actionlint.AnalysisSession, stdin io.Reader, paths []string, normalizeStdin bool) (*actionlint.AnalysisResult, error) {
	switch {
	case len(paths) == 0:
		return app.Repository("")
	case len(paths) == 1 && paths[0] == "-":
		return app.ReadStdin(stdin, normalizeStdin)
	default:
		return app.Files(paths, nil)
	}
}

func sameCommandFile(a, b string) bool {
	if absPath(a) == absPath(b) {
		return true
	}
	aInfo, aErr := os.Stat(a)
	bInfo, bErr := os.Stat(b)
	return aErr == nil && bErr == nil && os.SameFile(aInfo, bInfo)
}

func absPath(path string) string {
	if p, err := filepath.Abs(path); err == nil {
		return p
	}
	return path
}
