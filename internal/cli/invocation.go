package cli

import (
	"context"
	"errors"
	"fmt"

	"actionlint.kjanat.dev"
)

// invocation describes a CLI operation independently of its argument parser.
type invocation struct {
	Operation operation
	Check     checkInvocation
	Render    renderOptions
	JSON      bool
	Legacy    bool
	Origin    bool
	Rule      string
	Shell     string
}

// checkInvocation contains analysis inputs. It carries no command-framework state.
type checkInvocation struct {
	Paths         []string
	Config        actionlint.ConfigSelection
	StdinFilename string
	IgnoreRegex   []string
	ShellCheck    string
	Pyflakes      string
	Verbose       bool
	Debug         bool
}

// renderOptions controls result presentation without changing analysis.
type renderOptions struct {
	Hyperlinks   hyperlinkMode
	Format       actionlint.OutputFormat
	Template     string
	TemplateFile string
	OutputFile   string
	Color        actionlint.ColorOptionKind
	Oneline      bool
	Quiet        bool
	Summary      bool
}

func defaultInvocation() invocation {
	return invocation{
		Operation: "check",
		Check:     checkInvocation{StdinFilename: "<stdin>", ShellCheck: "shellcheck", Pyflakes: "pyflakes"},
	}
}

// operation is selected by a parser; execution rejects values outside this set.
type operation string

const (
	operationCheck          operation = "check"
	operationVersion        operation = "version"
	operationRules          operation = "rules"
	operationDoctor         operation = "doctor"
	operationConfigPath     operation = "config path"
	operationConfigShow     operation = "config show"
	operationConfigValidate operation = "config validate"
	operationConfigInit     operation = "config init"
	operationCompletion     operation = "completion"
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
