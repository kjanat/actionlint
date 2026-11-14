package cli

import (
	"errors"
	"fmt"
	"slices"

	commandargs "actionlint.kjanat.dev/v2/internal/cli/arguments"
	plugincmd "actionlint.kjanat.dev/v2/internal/cli/plugin"

	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/outputformat"
	render "actionlint.kjanat.dev/v2/internal/output"
)

// invocation holds parser state until it is converted to an operation-specific request.
type invocation struct {
	Operation      operation
	Check          checkInvocation
	Plugin         pluginInvocation
	Cache          cacheInvocation
	Render         renderOptions
	Yes            bool
	JSON           bool
	Legacy         bool
	Origin         bool
	MigrationWrite bool
	MigrationCheck bool
	ConfigFormat   repoconfig.FileFormat
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
	Offline               bool
	WarmCache             bool
	Fix                   bool
	ApplyFixes            bool
	IncludeSuggestedFixes bool
	Paths                 []string
	Config                repoconfig.Selection
	StdinFilename         string
	IgnoreRegex           []string
	IgnoreSelectors       []string
	// ToolFlags lists the --tool values, ID or ID=COMMAND, in command-line
	// order. --no-tool ID and the deprecated command flags, such as
	// --shellcheck=COMMAND, add their values here too.
	ToolFlags []string
	NoTools   bool
	// DeprecatedToolFlags lists uses of deprecated flags whose value is
	// ignored, such as --pyflakes.
	DeprecatedToolFlags []deprecatedToolFlag
	// Tools selects every bundled tool. applyEnvironment sets it, and the
	// two lists below, from the flags above and the environment.
	Tools registry.Selections
	// DisabledTools lists the tool ids the flags disabled.
	DisabledTools []string
	// NamedTools lists the tool ids --tool and --no-tool named.
	NamedTools []string
	// Deprecations holds a warning for each deprecated flag used.
	Deprecations         []string
	Plugins              []string
	PluginLock           string
	Verbose              bool
	Debug                bool
	Strict               bool
	Experimental         bool
	ExperimentalSet      bool
	PreCommit            bool
	NoIgnore             bool
	AllowConfigProcesses bool
}

// renderOptions controls result presentation without changing analysis.
type renderOptions struct {
	PrettyJSON         bool
	PrettyJSONExplicit bool
	Hyperlinks         hyperlinkMode
	Formats            []render.Format
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
	operationPluginValidate operation = "plugin validate"
	operationPluginTest     operation = "plugin test"
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
	operationConfigSchema   operation = "config schema"
	operationConfigMigrate  operation = "config migrate"
	operationConfigInit     operation = "config init"
	operationCompletion     operation = "completion"
	operationCacheDir       operation = "cache dir"
	operationCacheList      operation = "cache list"
	operationCachePrune     operation = "cache prune"
	operationCacheClean     operation = "cache clean"
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
		return commandargs.UsageError{Cause: errors.New("--output-file is only supported for checks")}
	}
	if i.Operation == "version" || i.Operation == "completion" {
		return nil
	}
	if err := a.prepareNetwork(); err != nil {
		return err
	}
	if i.Check.Config.Disabled && i.Check.Config.Path != "" {
		return commandargs.UsageError{Cause: errors.New("--config cannot be combined with --no-config")}
	}
	if i.Operation != "check" && !i.Legacy {
		return nil
	}
	explicitOutput := a.set["output-format"] || a.set["output"] || a.set["o"]
	if jsonRequested && explicitOutput {
		for _, mode := range o.output {
			if mode != "json" {
				return commandargs.UsageError{Cause: errors.New("--json cannot be combined with a different --output-format mode")}
			}
		}
	}
	if i.JSON && !explicitOutput {
		o.output = outputModes{"json"}
	}
	if i.Render.Template != "" && i.Render.TemplateFile != "" {
		return commandargs.UsageError{Cause: errors.New("--template cannot be combined with --template-file")}
	}
	if (i.Render.Template != "" || i.Render.TemplateFile != "") && (i.JSON || explicitOutput) {
		return commandargs.UsageError{Cause: errors.New("--template/--format cannot be combined with --json or --output-format")}
	}
	if !explicitOutput && !i.JSON && i.Render.Template == "" && i.Render.TemplateFile == "" && !i.Render.Oneline {
		o.output = outputModes{"pretty"}
	}
	if a.set["diagnostic-width"] && !slices.Contains(o.output, "pretty") {
		return commandargs.UsageError{Cause: errors.New("--diagnostic-width requires --output-format=pretty")}
	}
	if i.Render.Width < 40 || i.Render.Width > 400 {
		return commandargs.UsageError{Cause: errors.New("--diagnostic-width must be between 40 and 400")}
	}
	for _, mode := range o.output {
		if outputformat.Valid(mode) {
			i.Render.Oneline = false
			format := render.Format(mode)
			if !slices.Contains(i.Render.Formats, format) {
				i.Render.Formats = append(i.Render.Formats, format)
			}
		} else {
			return commandargs.UsageError{Cause: fmt.Errorf("invalid output mode %q: choose text, pretty, oneline, gcc, json, jsonl, sarif or github", mode)}
		}
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
			return commandargs.UsageError{Cause: fmt.Errorf("invalid log level %q: choose none, info or debug", o.logLevel)}
		}
	}
	return nil
}

func (inv invocation) request() (commandRequest, error) {
	switch inv.Operation {
	case operationCacheDir, operationCacheList, operationCachePrune, operationCacheClean:
		if inv.Operation == operationCachePrune && inv.Cache.OlderThan <= 0 {
			return nil, commandargs.UsageError{Cause: errors.New("cache prune --older-than must be positive")}
		}
		return cacheRequest{Operation: inv.Operation, cacheInvocation: inv.Cache, JSON: inv.JSON}, nil
	case operationPluginInspect:
		return pluginInspectRequest{Source: inv.Check.Paths[0], JSON: inv.JSON, Registry: inv.Plugin.Registry}, nil
	case operationPluginValidate:
		path := ""
		if len(inv.Check.Paths) > 0 {
			path = inv.Check.Paths[0]
		}
		return pluginValidateRequest{Path: path, JSON: inv.JSON}, nil
	case operationPluginTest:
		return pluginTestRequest{Paths: inv.Check.Paths, JSON: inv.JSON}, nil
	case operationPluginSearch, operationPluginVersions, operationPluginInstall, operationPluginList, operationPluginUpdate, operationPluginRemove, operationPluginSync:
		return pluginRegistryRequest{Operation: plugincmd.Operation(inv.Operation), Options: plugincmd.RegistryOptions(inv.Plugin), Paths: inv.Check.Paths, LockFile: inv.Check.PluginLock, Config: inv.Check.Config, JSON: inv.JSON}, nil
	case "lsp":
		return lspRequest{inv.Check}, nil
	case operationVersion:
		return versionRequest{JSON: inv.JSON, Legacy: inv.Legacy}, nil
	case operationRules:
		return rulesRequest{Name: inv.Rule, JSON: inv.JSON, Check: inv.Check}, nil
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
	case operationConfigSchema:
		return configSchemaRequest{Config: inv.Check.Config, Plugins: inv.Check.Plugins}, nil
	case operationConfigMigrate:
		if inv.MigrationWrite && inv.MigrationCheck {
			return nil, commandargs.UsageError{Cause: errors.New("config migrate: --write and --check cannot be combined")}
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
			return nil, commandargs.UsageError{Cause: errors.New("config init always creates the repository config; omit --config and --no-config")}
		}
		return configInitRequest{JSON: inv.JSON, Hyperlinks: inv.Render.Hyperlinks, Format: inv.ConfigFormat}, nil
	case operationCheck:
		return checkRequest{Check: inv.Check, Render: inv.Render, Yes: inv.Yes, JSON: inv.JSON, Legacy: inv.Legacy}, nil
	case operationCompletion:
		// Completion output is not a command request.
		fallthrough
	default:
		return nil, commandargs.UsageError{Cause: fmt.Errorf("unknown operation %q", inv.Operation)}
	}
}
