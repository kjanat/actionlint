package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"actionlint.kjanat.dev"
	"github.com/fatih/color"
	"github.com/mattn/go-colorable"
)

func (a *commandApp) prepareInvocation() error {
	i, o := &a.inv, &a.opts
	jsonRequested := i.JSON
	i.JSON = a.jsonOutput()
	if !i.Legacy {
		switch i.Operation {
		case operationCompletion:
			i.Shell = i.Check.Paths[0]
		case operationRules:
			if len(i.Check.Paths) > 0 {
				i.Rule = i.Check.Paths[0]
			}
		}
	}
	if i.Operation != "check" && i.Render.OutputFile != "" {
		return commandUsageError{errors.New("--output-file is only supported for checks")}
	}
	if i.Operation == "version" || i.Operation == "completion" {
		return nil
	}
	if i.Check.Config.Disabled && i.Check.Config.Path != "" {
		return commandUsageError{errors.New("--config cannot be combined with --no-config")}
	}
	if i.Operation != "check" && !i.Legacy {
		return nil
	}
	explicitOutput := a.set["output-format"] || a.set["output"] || a.set["o"]
	if i.JSON && explicitOutput && o.output != "json" && o.output != "jsonl" {
		return commandUsageError{errors.New("--json cannot be combined with a different --output-format mode")}
	}
	if jsonRequested && explicitOutput && o.output == "jsonl" {
		return commandUsageError{errors.New("--json cannot be combined with a different --output-format mode")}
	}
	if i.JSON && !explicitOutput {
		o.output = "json"
	}
	if i.Render.Template != "" && i.Render.TemplateFile != "" {
		return commandUsageError{errors.New("--template cannot be combined with --template-file")}
	}
	if (i.Render.Template != "" || i.Render.TemplateFile != "") && (i.JSON || explicitOutput) {
		return commandUsageError{errors.New("--template/--format cannot be combined with --json or --output-format")}
	}
	switch o.output {
	case "text", "oneline", "json", "jsonl", "sarif", "github":
		if explicitOutput || i.JSON {
			i.Render.Oneline = false
			i.Render.Format = actionlint.OutputFormat(o.output)
		}
	default:
		return commandUsageError{fmt.Errorf("invalid output mode %q: choose text, oneline, json, jsonl, sarif or github", o.output)}
	}
	if a.set["log-level"] {
		switch o.logLevel {
		case "none":
			i.Check.Verbose, i.Check.Debug = false, false
		case "info":
			i.Check.Verbose, i.Check.Debug = true, false
		case "debug":
			i.Check.Verbose, i.Check.Debug = false, true
		default:
			return commandUsageError{fmt.Errorf("invalid log level %q: choose none, info or debug", o.logLevel)}
		}
	}
	if o.color {
		i.Render.Color = actionlint.ColorOptionKindAlways
	}
	if !i.Legacy && a.set["modern-color"] {
		switch o.colorMode {
		case "auto":
			i.Render.Color = actionlint.ColorOptionKindAuto
		case "always":
			i.Render.Color = actionlint.ColorOptionKindAlways
		case "never":
			i.Render.Color = actionlint.ColorOptionKindNever
		default:
			return commandUsageError{fmt.Errorf("invalid color mode %q: choose auto, always or never", o.colorMode)}
		}
	}
	if o.noColor {
		i.Render.Color = actionlint.ColorOptionKindNever
	}
	return nil
}

func executeInvocation(ctx context.Context, streams Command, inv invocation) (int, error) {
	switch inv.Operation {
	case operationVersion:
		return 0, writeVersion(streams.Stdout, inv.JSON, inv.Legacy)
	case operationRules:
		return 0, writeRules(streams.Stdout, inv.Rule, inv.JSON)
	case operationDoctor:
		return 0, writeDoctor(streams.Stdout, inv.Check, inv.JSON)
	case operationConfigPath, operationConfigShow, operationConfigValidate:
		return 0, runConfigCommand(streams.Stdout, inv)
	case operationConfigInit:
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		return 0, initCommandConfig(streams, inv)
	case operationCheck:
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		return executeCheck(ctx, streams, inv)
	default:
		return 0, commandUsageError{fmt.Errorf("unknown operation %q", inv.Operation)}
	}
}

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

type commandResultWriter struct {
	io.Writer
	err error
}

func (w *commandResultWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if err == nil && n < len(p) {
		err = io.ErrShortWrite
	}
	if w.err == nil {
		w.err = err
	}
	return n, err
}

func sameCommandFile(a, b string) bool {
	if absPath(a) == absPath(b) {
		return true
	}
	aInfo, aErr := os.Stat(a)
	bInfo, bErr := os.Stat(b)
	return aErr == nil && bErr == nil && os.SameFile(aInfo, bInfo)
}

func resolveTemplate(r renderOptions) (renderOptions, error) {
	if r.TemplateFile != "" {
		data, err := os.ReadFile(r.TemplateFile)
		if err != nil {
			return r, fmt.Errorf("could not read template file %q: %w", r.TemplateFile, err)
		}
		r.Template = string(data)
		if r.Template == "" {
			return r, fmt.Errorf("template file %q is empty", r.TemplateFile)
		}
	}
	return r, nil
}

func absPath(path string) string {
	if p, err := filepath.Abs(path); err == nil {
		return p
	}
	return path
}
