package cli

import (
	"errors"
	"fmt"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/render"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
)

// invocation holds parser state until it is converted to an operation-specific request.
type invocation struct {
	Operation      operation
	Check          checkInvocation
	Plugin         pluginInvocation
	Render         renderOptions
	Yes            bool
	JSON           bool
	Legacy         bool
	Origin         bool
	MigrationWrite bool
	MigrationCheck bool
	Rule           string
	Shell          string
}

// checkInvocation contains analysis inputs. It carries no command-framework state.
type checkInvocation struct {
	RunnerTarget          string
	RunnerProvider        string
	RunnerSnapshot        string
	RunnerScope           string
	ResolverCommand       string
	ResolverArguments     []string
	Fix                   bool
	ApplyFixes            bool
	IncludeSuggestedFixes bool
	Paths                 []string
	Config                repoconfig.Selection
	StdinFilename         string
	IgnoreRegex           []string
	// ToolFlags lists the --tool values, ID or ID=COMMAND, in command-line
	// order. --no-tool ID and the deprecated --ID and --no-ID flags add
	// their values here too.
	ToolFlags []string
	// DeprecatedToolFlags lists uses of retired flags that bundled provider
	// manifests declare.
	DeprecatedToolFlags []deprecatedToolFlag
	// Tools selects every bundled tool. applyEnvironment sets it, and the
	// two lists below, from the flags above and the environment.
	Tools registry.Selections
	// DisabledTools lists the tool ids the flags disabled.
	DisabledTools []string
	// NamedTools lists the tool ids --tool and --no-tool named.
	NamedTools []string
	// Deprecations holds a warning for each deprecated flag or environment
	// variable used.
	Deprecations         []string
	Plugins              []string
	PluginLock           string
	Verbose              bool
	Debug                bool
	Strict               bool
	Experimental         bool
	ExperimentalSet      bool
	PreCommit            bool
	AllowConfigProcesses bool
}

// renderOptions controls result presentation without changing analysis.
type renderOptions struct {
	PrettyJSON         bool
	PrettyJSONExplicit bool
	Hyperlinks         hyperlinkMode
	Format             render.Format
	Template           string
	TemplateFile       string
	OutputFile         string
	Color              render.Color
	Width              int
	Oneline            bool
	Quiet              bool
	Summary            bool
}

func defaultInvocation() invocation {
	return invocation{
		Operation: "check",
		Render:    renderOptions{PrettyJSON: true, Summary: true, Width: 100},
		Check:     checkInvocation{StdinFilename: "<stdin>"},
	}
}

// operation is selected by a parser; execution rejects values outside this set.
type operation string

const (
	operationCheck          operation = "check"
	operationVersion        operation = "version"
	operationRules          operation = "rules"
	operationPluginInspect  operation = "plugin inspect"
	operationPluginSearch   operation = "plugin search"
	operationPluginVersions operation = "plugin versions"
	operationPluginInstall  operation = "plugin install"
	operationPluginList     operation = "plugin list"
	operationPluginUpdate   operation = "plugin update"
	operationPluginRemove   operation = "plugin remove"
	operationPluginSync     operation = "plugin sync"
	operationDoctor         operation = "doctor"
	operationConfigPath     operation = "config path"
	operationConfigShow     operation = "config show"
	operationConfigValidate operation = "config validate"
	operationConfigMigrate  operation = "config migrate"
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
	if !i.Legacy && i.Operation == operationCompletion {
		i.Shell = i.Check.Paths[0]
	}
	if !i.Legacy && i.Operation == operationRules && len(i.Check.Paths) > 0 {
		i.Rule = i.Check.Paths[0]
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
	if a.set["diagnostic-width"] && o.output != "pretty" {
		return commandUsageError{errors.New("--diagnostic-width requires --output-format=pretty")}
	}
	if i.Render.Width < 40 || i.Render.Width > 400 {
		return commandUsageError{errors.New("--diagnostic-width must be between 40 and 400")}
	}
	switch o.output {
	case "text", "pretty", "oneline", "json", "jsonl", "sarif", "github":
		if explicitOutput || i.JSON {
			i.Render.Oneline = false
			i.Render.Format = render.Format(o.output)
		}
	default:
		return commandUsageError{fmt.Errorf("invalid output mode %q: choose text, pretty, oneline, json, jsonl, sarif or github", o.output)}
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
	case operationPluginInspect:
		return pluginInspectRequest{Source: inv.Check.Paths[0], JSON: inv.JSON}, nil
	case operationPluginSearch, operationPluginVersions, operationPluginInstall, operationPluginList, operationPluginUpdate, operationPluginRemove, operationPluginSync:
		return pluginRegistryRequest{Operation: inv.Operation, Options: inv.Plugin, Paths: inv.Check.Paths, LockFile: inv.Check.PluginLock, Config: inv.Check.Config, JSON: inv.JSON}, nil
	case "lsp":
		return lspRequest{inv.Check}, nil
	case operationVersion:
		return versionRequest{JSON: inv.JSON, Legacy: inv.Legacy}, nil
	case operationRules:
		return rulesRequest{Name: inv.Rule, JSON: inv.JSON, Plugins: inv.Check.Plugins, Selection: inv.Check.pluginSelection()}, nil
	case operationDoctor:
		return doctorRequest{
			ResolverCommand: inv.Check.ResolverCommand, ResolverArguments: inv.Check.ResolverArguments,
			Config: inv.Check.Config, Plugins: inv.Check.Plugins, Tools: inv.Check.Tools,
			NamedTools: inv.Check.NamedTools, Deprecations: inv.Check.Deprecations,
			JSON: inv.JSON, Hyperlinks: inv.Render.Hyperlinks,
		}, nil
	case operationConfigPath:
		return configPathRequest{Config: inv.Check.Config, JSON: inv.JSON}, nil
	case operationConfigShow:
		return configShowRequest{Config: inv.Check.Config, Plugins: inv.Check.Plugins, JSON: inv.JSON, Origin: inv.Origin}, nil
	case operationConfigValidate:
		return configValidateRequest{Config: inv.Check.Config, Plugins: inv.Check.Plugins, JSON: inv.JSON}, nil
	case operationConfigMigrate:
		if inv.MigrationWrite && inv.MigrationCheck {
			return nil, commandUsageError{errors.New("config migrate: --write and --check cannot be combined")}
		}
		return configMigrateRequest{Config: inv.Check.Config, JSON: inv.JSON, Write: inv.MigrationWrite, Check: inv.MigrationCheck}, nil
	case operationConfigInit:
		if inv.Legacy {
			return legacyConfigInitRequest{
				ConfigPath: inv.Check.Config.Path, IgnoreRegex: inv.Check.IgnoreRegex,
				Template: inv.Render.Template, TemplateFile: inv.Render.TemplateFile,
				Log: (inv.Check.Verbose || inv.Check.Debug) && !inv.Render.Quiet, JSON: inv.JSON, Hyperlinks: inv.Render.Hyperlinks,
			}, nil
		}
		if inv.Check.Config.Path != "" || inv.Check.Config.Disabled {
			return nil, commandUsageError{errors.New("config init always creates the repository config; omit --config and --no-config")}
		}
		return configInitRequest{JSON: inv.JSON, Hyperlinks: inv.Render.Hyperlinks}, nil
	case operationCheck:
		return checkRequest{Check: inv.Check, Render: inv.Render, Yes: inv.Yes, JSON: inv.JSON, Legacy: inv.Legacy}, nil
	case operationCompletion:
		// Completion output is not a command request.
		fallthrough
	default:
		return nil, commandUsageError{fmt.Errorf("unknown operation %q", inv.Operation)}
	}
}
