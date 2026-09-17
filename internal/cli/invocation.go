package cli

import (
	"errors"
	"fmt"

	"actionlint.kjanat.dev"
)

// invocation holds parser state until it is converted to an operation-specific request.
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
	Paths             []string
	Config            actionlint.ConfigSelection
	StdinFilename     string
	IgnoreRegex       []string
	ShellCheck        string
	Pyflakes          string
	ShellcheckOptions *actionlint.ExternalCommandOptions
	PyflakesOptions   *actionlint.ExternalCommandOptions
	Verbose           bool
	Debug             bool
}

// renderOptions controls result presentation without changing analysis.
type renderOptions struct {
	PrettyJSON   bool
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
		Render:    renderOptions{PrettyJSON: true},
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
	if err := a.prepareColor(); err != nil {
		return err
	}
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
	return nil
}

func (inv invocation) request() (commandRequest, error) {
	switch inv.Operation {
	case operationVersion:
		return versionRequest{JSON: inv.JSON, Legacy: inv.Legacy}, nil
	case operationRules:
		return rulesRequest{Name: inv.Rule, JSON: inv.JSON}, nil
	case operationDoctor:
		return doctorRequest{Config: inv.Check.Config, ShellCheck: inv.Check.ShellCheck, Pyflakes: inv.Check.Pyflakes,
			ShellcheckOptions: inv.Check.ShellcheckOptions, PyflakesOptions: inv.Check.PyflakesOptions,
			JSON: inv.JSON, Hyperlinks: inv.Render.Hyperlinks}, nil
	case operationConfigPath:
		return configPathRequest{Config: inv.Check.Config, JSON: inv.JSON}, nil
	case operationConfigShow:
		return configShowRequest{Config: inv.Check.Config, JSON: inv.JSON, Origin: inv.Origin}, nil
	case operationConfigValidate:
		return configValidateRequest{Config: inv.Check.Config, JSON: inv.JSON}, nil
	case operationConfigInit:
		if inv.Legacy {
			return legacyConfigInitRequest{ConfigPath: inv.Check.Config.Path, IgnoreRegex: inv.Check.IgnoreRegex,
				Template: inv.Render.Template, TemplateFile: inv.Render.TemplateFile,
				Log: (inv.Check.Verbose || inv.Check.Debug) && !inv.Render.Quiet, JSON: inv.JSON}, nil
		}
		if inv.Check.Config.Path != "" || inv.Check.Config.Disabled {
			return nil, commandUsageError{errors.New("config init always creates the repository config; omit --config and --no-config")}
		}
		return configInitRequest{JSON: inv.JSON}, nil
	case operationCheck:
		return checkRequest{Check: inv.Check, Render: inv.Render, JSON: inv.JSON, Legacy: inv.Legacy}, nil
	default:
		return nil, commandUsageError{fmt.Errorf("unknown operation %q", inv.Operation)}
	}
}
