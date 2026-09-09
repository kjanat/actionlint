package actionlint

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func (a *commandApp) prepareInvocation() error {
	i, o := &a.inv, &a.opts
	jsonRequested := i.JSON
	i.JSON = a.jsonOutput()
	if !i.Legacy {
		switch i.Operation {
		case "completion":
			i.Shell = i.Check.Paths[0]
		case "rules":
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
			i.Render.Format = OutputFormat(o.output)
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
		i.Render.Color = ColorOptionKindAlways
	}
	if !i.Legacy && a.set["modern-color"] {
		switch o.colorMode {
		case "auto":
			i.Render.Color = ColorOptionKindAuto
		case "always":
			i.Render.Color = ColorOptionKindAlways
		case "never":
			i.Render.Color = ColorOptionKindNever
		default:
			return commandUsageError{fmt.Errorf("invalid color mode %q: choose auto, always or never", o.colorMode)}
		}
	}
	if o.noColor {
		i.Render.Color = ColorOptionKindNever
	}
	return nil
}

func executeInvocation(ctx context.Context, streams Command, inv Invocation) (int, error) {
	switch inv.Operation {
	case "version":
		return 0, writeVersion(streams.Stdout, inv.JSON, inv.Legacy)
	case "rules":
		return 0, writeRules(streams.Stdout, inv.Rule, inv.JSON)
	case "doctor":
		return 0, writeDoctor(streams.Stdout, inv.Check, inv.JSON)
	case "config path", "config show", "config validate":
		return 0, runConfigCommand(streams.Stdout, inv)
	case "config init":
		if !inv.Legacy {
			return 0, initCommandConfig(streams.Stdout, inv)
		}
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return executeCheck(ctx, streams, inv)
}

func executeCheck(ctx context.Context, streams Command, inv Invocation) (status int, err error) {
	c, r := inv.Check, inv.Render
	if r.TemplateFile != "" {
		data, err := os.ReadFile(r.TemplateFile)
		if err != nil {
			return 0, fmt.Errorf("could not read template file %q: %w", r.TemplateFile, err)
		}
		r.Template = string(data)
		if r.Template == "" {
			return 0, fmt.Errorf("template file %q is empty", r.TemplateFile)
		}
	}
	// A completed analysis replaces the previous report atomically.
	out := streams.Stdout
	if r.OutputFile != "" && r.OutputFile != "-" {
		file, e := os.CreateTemp(filepath.Dir(r.OutputFile), ".actionlint-report-*")
		if e != nil {
			return 0, e
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
		if r.Color == ColorOptionKindAuto {
			r.Color = ColorOptionKindNever
		}
	}
	log := streams.Stderr
	if inv.JSON {
		log = &commandJSONLogWriter{out: log}
	}
	options := LinterOptions{
		Context: ctx, LogWriter: log, Color: r.Color, Oneline: r.Oneline,
		Shellcheck: c.ShellCheck, Pyflakes: c.Pyflakes, ConfigFile: c.Config.Path,
		IgnorePatterns: c.IgnoreRegex, StdinFileName: c.StdinFilename,
		Format: r.Template, OutputFormat: r.Format,
		Verbose: c.Verbose && !r.Quiet, Debug: c.Debug && !r.Quiet,
	}
	selected := 0
	var inputPaths []string
	options.OnFilesSelected = func(paths []string) { selected = len(paths); inputPaths = paths }
	l, err := NewLinter(out, &options)
	if err != nil {
		return 0, err
	}
	var writes *commandResultWriter
	if !inv.Legacy || r.OutputFile != "" {
		writes = &commandResultWriter{Writer: l.out}
		l.out = writes
	}
	if c.Config.Disabled || (!inv.Legacy && c.Config.Path != "") {
		l.projects.skipConfig = true
	}
	if inv.Operation == "config init" {
		if inv.JSON {
			path, err := l.generateDefaultConfig("")
			if err != nil {
				return 0, err
			}
			return 0, writeCommandJSON(out, map[string]string{"path": path})
		}
		return 0, l.GenerateDefaultConfig("")
	}
	var findings []*Error
	switch {
	case len(c.Paths) == 0:
		findings, err = l.LintRepository("")
	case len(c.Paths) == 1 && c.Paths[0] == "-":
		selected = 1
		findings, err = l.LintStdin(streams.Stdin)
	default:
		findings, err = l.LintFiles(c.Paths, nil)
	}
	if err != nil {
		return 0, err
	}
	if writes != nil && writes.err != nil {
		return 0, writes.err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if r.OutputFile != "" && r.OutputFile != "-" {
		inputPaths = append(inputPaths, c.Config.Path, r.TemplateFile)
		for _, project := range l.projects.known {
			if project.Config() != nil {
				for _, name := range []string{"actionlint.yaml", "actionlint.yml"} {
					inputPaths = append(inputPaths, filepath.Join(project.RootDir(), ".github", name))
				}
			}
		}
		for _, path := range inputPaths {
			if path != "" && sameCommandFile(path, r.OutputFile) {
				return 0, fmt.Errorf("output file %q is also an input", r.OutputFile)
			}
		}
	}
	if len(findings) > 0 {
		status = ExitStatusSuccessProblemFound
	}
	if r.Summary && !r.Quiet {
		summary := CheckSummary{Files: selected, Findings: len(findings)}
		if inv.JSON {
			err = writeCommandJSON(streams.Stderr, map[string]CheckSummary{"summary": summary})
		} else {
			_, err = fmt.Fprintf(streams.Stderr, "Checked %d workflows; %d findings.\n", selected, len(findings))
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
