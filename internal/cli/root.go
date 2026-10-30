package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type commandOptions struct {
	output, colorMode, logLevel, command            string
	color, noColor, initConfig, version, helpLegacy bool
	noRuff, noShellCheck                            bool
	completion                                      completionShell
}

type commandApp struct {
	// ctx is the invocation context. Cobra leaves Command.Context nil for
	// commands it did not execute, such as help targets, so help output uses this.
	ctx          context.Context
	root         *cobra.Command
	streams      Command
	inv          invocation
	opts         commandOptions
	set          map[string]bool
	status       int
	errorJSON    bool
	helpShown    bool
	prefixIgnore []string
}

type commandUsageError struct{ error }

const commandGroupAnnotation = "actionlint/group"

const commandChoicesAnnotation = "actionlint/choices"

const commandOptionalValueAnnotation = "actionlint/optional-value"

func newCommandApp(ctx context.Context, streams *Command) *commandApp {
	a := &commandApp{ctx: ctx, streams: *streams, inv: defaultInvocation(), set: map[string]bool{}}
	if a.streams.Stdin == nil {
		a.streams.Stdin = strings.NewReader("")
	}
	if a.streams.Stdout == nil {
		a.streams.Stdout = io.Discard
	}
	if a.streams.Stderr == nil {
		a.streams.Stderr = io.Discard
	}
	shell, _ := currentCompletionShell()
	a.root = &cobra.Command{
		Use:           "actionlint [flags] [--] [files... | -]",
		Short:         "Check GitHub Actions workflows",
		Long:          "Check workflows, actions and embedded scripts.\nRun actionlint check to discover workflows and actions in the current repository.\nWith no arguments, show help. Pass - alone to read stdin.",
		SilenceErrors: true, SilenceUsage: true, DisableFlagsInUseLine: true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		RunE:              a.capture("check"),
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: completeRootInputs,
	}
	a.root.SetIn(a.streams.Stdin)
	a.root.SetOut(a.streams.Stdout)
	a.root.SetErr(a.streams.Stderr)
	a.root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return commandUsageError{err} })
	a.root.SetHelpFunc(a.help)
	a.checkFlags(a.root, false)
	a.root.Flags().SetInterspersed(true)
	check := &cobra.Command{
		Use: "check [flags] [--] [files... | -]", Short: "Check workflows and actions",
		Long: "Check workflow and action files, stdin, or the current repository.\nOptions may appear before or after filenames. Directory inputs are not expanded.",
		Args: cobra.ArbitraryArgs, RunE: a.capture("check"),
		ValidArgsFunction: cobra.FixedCompletions([]string{"yaml", "yml"}, cobra.ShellCompDirectiveFilterFileExt),
	}
	a.checkFlags(check, true)
	fix := &cobra.Command{
		Use: "fix [flags] [--] [files...]", Short: "Preview or apply suggested source edits", Args: cobra.ArbitraryArgs,
		RunE: func(c *cobra.Command, args []string) error {
			a.inv.Check.Fix = true
			return a.capture(operationCheck)(c, args)
		},
		ValidArgsFunction: cobra.FixedCompletions([]string{"yaml", "yml"}, cobra.ShellCompDirectiveFilterFileExt),
	}
	a.checkFlags(fix, true)
	fix.Flags().BoolVar(&a.inv.Check.ApplyFixes, "apply", false, "Apply edits and check the updated files")
	fix.Flags().BoolVar(&a.inv.Check.IncludeSuggestedFixes, "include-suggested", false, "Include edits that require manual review")
	annotateFlag(fix.Flags(), "apply", "Fixes")
	annotateFlag(fix.Flags(), "include-suggested", "Fixes")
	preCommit := &cobra.Command{
		Use: "pre-commit [flags]", Short: "Check the repository for a pre-commit hook", Hidden: true,
		Long: "Check every workflow in the nearest repository.\nA repository without workflow files passes.\nArguments that Git passes to a hook are ignored.",
		Args: cobra.ArbitraryArgs, RunE: a.capture("check"),
		// git rebase --root passes --root to pre-rebase hooks.
		FParseErrWhitelist: cobra.FParseErrWhitelist{UnknownFlags: true},
	}
	a.checkFlags(preCommit, true)
	config := &cobra.Command{
		Use: "config", Short: "Inspect and validate configuration",
		Long: "Select a configuration file or the repository's .github/actionlint.yaml, .yml, .json or .toml.\nUse extends for shared configuration and overrides for file-specific settings.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			if a.opts.initConfig {
				return a.capture(operationConfigInit)(c, args)
			}
			a.help(c, args)
			return nil
		},
	}
	for _, name := range []string{"init", "init-config"} {
		config.PersistentFlags().BoolVar(&a.opts.initConfig, name, false, "Alias for config init")
		_ = config.PersistentFlags().MarkHidden(name)
	}
	a.configFlags(config.PersistentFlags())
	a.commonFlags(config, config.PersistentFlags())
	for _, item := range []struct{ name, description string }{
		{"init", "Create the repository config with its editor schema directive"},
		{"path", "Print the selected configuration path"},
		{"show", "Show effective configuration settings"},
		{"validate", "Validate the selected configuration using actionlint's loader"},
		{"migrate", "Preview conversion to current configuration syntax"},
	} {
		c := &cobra.Command{Use: item.name, Short: item.description, Args: cobra.NoArgs, RunE: a.capture(operation("config " + item.name))}
		if item.name == "show" || item.name == "validate" {
			a.pluginFlags(c.Flags())
		}
		if item.name == "show" {
			c.Flags().BoolVar(&a.inv.Origin, "origin", false, "Include the source of each setting")
			annotateFlag(c.Flags(), "origin", "Input")
		}
		if item.name == "migrate" {
			c.Flags().BoolVar(&a.inv.MigrationWrite, "write", false, "Atomically write the migrated configuration")
			c.Flags().BoolVar(&a.inv.MigrationCheck, "check", false, "Exit with status 1 when migration is needed")
			annotateFlag(c.Flags(), "write", "Output")
			annotateFlag(c.Flags(), "check", "Output")
		}
		config.AddCommand(c)
	}
	rules := &cobra.Command{
		Use: "rules [rule]", Short: "List checks or explain one check",
		Args: cobra.MaximumNArgs(1), RunE: a.capture("rules"),
		ValidArgsFunction: func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			catalog := commandRules()
			names := make([]string, 0, len(catalog))
			for _, r := range catalog {
				names = append(names, r.Name)
			}
			return names, cobra.ShellCompDirectiveNoFileComp
		},
	}
	a.commonFlags(rules, rules.Flags())
	rules.Flags().StringArrayVar(&a.inv.Check.Plugins, "plugin", nil, "Include rules from a WebAssembly plugin")
	annotateFlag(rules.Flags(), "plugin", "Rule selection")
	a.configFlags(rules.Flags())
	a.pluginLockFlag(rules.Flags())
	plugins := a.pluginCommand()
	doctor := &cobra.Command{Use: "doctor", Short: "Inspect configuration and external tool availability", Args: cobra.NoArgs, RunE: a.capture("doctor")}
	a.commonFlags(doctor, doctor.Flags())
	a.configFlags(doctor.Flags())
	a.toolFlags(doctor.Flags())
	a.pluginFlags(doctor.Flags())
	completion := &cobra.Command{
		Use: "completion <shell>", Aliases: []string{"completions"}, Short: "Generate shell completions",
		Long:    "Supported shells: bash, zsh, fish, powershell (alias: pwsh).\nUse auto to detect your shell, or pass its executable path.",
		Example: completionExamples(shell),
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 1 {
				return fmt.Errorf("completion accepts one shell: %s, pwsh or auto\nUsage: actionlint completion <shell>", completionShellNames())
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				a.help(c, args)
				return nil
			}
			return a.capture(operationCompletion)(c, args)
		},
		ValidArgsFunction: cobra.FixedCompletions(append(completionShellNameList(), "pwsh", "auto"), cobra.ShellCompDirectiveNoFileComp),
	}
	version := &cobra.Command{Use: "version", Short: "Show version and build information", Args: cobra.NoArgs, RunE: a.capture("version")}
	a.commonFlags(version, version.Flags())
	a.hyperlinkFlag(completion, completion.Flags())
	init := &cobra.Command{
		Use: "init", Aliases: []string{"init-config"}, Hidden: true,
		Short: "Alias for config init", Args: cobra.NoArgs, RunE: a.capture(operationConfigInit),
	}
	a.commonFlags(init, init.Flags())
	lsp := &cobra.Command{Use: "lsp", Short: "Serve the language server over stdin/stdout", Args: cobra.NoArgs, RunE: a.capture("lsp")}
	a.configFlags(lsp.Flags())
	a.toolFlags(lsp.Flags())
	a.runnerFlags(lsp.Flags())
	lsp.Flags().StringArrayVar(&a.inv.Check.Plugins, "plugin", nil, "Load a WebAssembly analyzer or script engine from a path or URI")
	annotateFlag(lsp.Flags(), "plugin", "Rule selection")
	a.pluginLockFlag(lsp.Flags())
	a.root.AddCommand(plugins, lsp, check, fix, preCommit, config, rules, doctor, completion, version, init)
	return a
}

func annotateFlag(f *pflag.FlagSet, name, group string) {
	f.Lookup(name).Annotations = map[string][]string{commandGroupAnnotation: {group}}
}

func (a *commandApp) commonFlags(c *cobra.Command, f *pflag.FlagSet) {
	f.BoolVar(&a.inv.JSON, "json", false, "Write JSON output")
	f.BoolVar(&a.inv.Render.PrettyJSON, "json-pretty", true, "Indent JSON output")
	f.BoolVarP(&a.inv.Render.Quiet, "quiet", "q", false, "Suppress progress and summaries; keep findings and errors")
	annotateFlag(f, "json", "Output")
	annotateFlag(f, "json-pretty", "Output")
	annotateFlag(f, "quiet", "Output")
	a.hyperlinkFlag(c, f)
}

func (a *commandApp) hyperlinkFlag(c *cobra.Command, f *pflag.FlagSet) {
	f.Var(&a.inv.Render.Hyperlinks, "hyperlinks", "Hyperlinks in help and doctor: auto, always or never")
	annotateFlag(f, "hyperlinks", "Output")
	choices := []string{"auto", "always", "never"}
	f.Lookup("hyperlinks").Annotations[commandChoicesAnnotation] = choices
	if err := c.RegisterFlagCompletionFunc("hyperlinks", cobra.FixedCompletions(choices, cobra.ShellCompDirectiveNoFileComp)); err != nil {
		panic(err)
	}
}

func (a *commandApp) configFlags(f *pflag.FlagSet) {
	f.StringVar(&a.inv.Check.Config.Path, "config", "", "Read configuration from `PATH`")
	f.BoolVar(&a.inv.Check.Config.Disabled, "no-config", false, "Use default configuration")
	f.StringVar(&a.inv.Check.Config.Path, "config-file", "", "Alias for --config")
	_ = f.MarkHidden("config-file")
	for _, name := range []string{"config", "config-file", "no-config"} {
		annotateFlag(f, name, "Configuration")
	}
}

func (a *commandApp) toolFlags(f *pflag.FlagSet) {
	f.StringVar(&a.inv.Check.ResolverCommand, "resolve-with", "", "Resolve remote dependencies using this executable")
	f.StringArrayVar(&a.inv.Check.ResolverArguments, "resolver-arg", nil, "Pass an argument to the remote resolver (repeatable)")
	annotateFlag(f, "resolve-with", "Dependency resolution")
	annotateFlag(f, "resolver-arg", "Dependency resolution")
	f.StringVar(&a.inv.Check.Ruff, "ruff", "ruff", "Check Python scripts")
	f.StringVar(&a.inv.Check.ShellCheck, "shellcheck", "shellcheck", "Check shell scripts")
	f.BoolVar(&a.opts.noRuff, "no-ruff", false, "Disable Python script linting")
	f.BoolVar(&a.opts.noShellCheck, "no-shellcheck", false, "Disable shell script linting")
	f.BoolVar(&a.inv.Check.AllowConfigProcesses, "allow-config-processes", false, "Allow trusted repository configuration to select native executables")
	for _, name := range []string{"ruff", "shellcheck", "no-ruff", "no-shellcheck", "allow-config-processes"} {
		annotateFlag(f, name, "External linters")
	}
	for _, name := range []string{"ruff", "shellcheck"} {
		f.Lookup(name).NoOptDefVal = name
		f.Lookup(name).Annotations[commandOptionalValueAnnotation] = []string{"COMMAND"}
	}
	f.StringVar(&a.inv.Check.Pyflakes, "pyflakes", "", "Deprecated: select Pyflakes rules through Ruff")
	_ = f.MarkHidden("pyflakes")
	annotateFlag(f, "pyflakes", "External linters")
}

func (a *commandApp) pluginFlags(f *pflag.FlagSet) {
	f.StringArrayVar(&a.inv.Check.Plugins, "plugin", nil, "Load a WebAssembly analyzer or script engine from a path or `URI`, or a local tool provider manifest (.json) (repeatable)")
	annotateFlag(f, "plugin", "Rule selection")
}

func (a *commandApp) checkFlags(c *cobra.Command, modern bool) {
	f := c.Flags()
	a.commonFlags(c, f)
	a.configFlags(f)
	a.toolFlags(f)
	a.pluginFlags(f)
	f.BoolVar(&a.inv.Check.Strict, "strict", false, "Enable all stable rules")
	f.BoolVar(&a.inv.Check.Experimental, "experimental", false, "Enable nursery rules")
	annotateFlag(f, "strict", "Rule selection")
	annotateFlag(f, "experimental", "Rule selection")
	f.StringVar(&a.inv.Check.StdinFilename, "stdin-filename", "<stdin>", "Use `FILE` as the stdin filename")
	a.pluginLockFlag(f)
	a.runnerFlags(f)
	f.StringArrayVar(&a.inv.Check.IgnoreRegex, "ignore-regex", nil, "Ignore messages matching `REGEX` (repeatable)")
	f.Var(f.Lookup("ignore-regex").Value, "ignore", "Alias for --ignore-regex")
	f.StringVarP(&a.opts.output, "output-format", "o", "text", "Output mode: text, pretty, oneline, json, jsonl, sarif or github")
	f.IntVar(&a.inv.Render.Width, "diagnostic-width", 100, "Pretty diagnostic width in columns (40-400)")
	f.StringVar(&a.opts.output, "output", "text", "Alias for --output-format")
	f.StringVarP(&a.inv.Render.Template, "template", "f", "", "Render legacy fields with this Go template")
	f.StringVar(&a.inv.Render.Template, "format", "", "Alias for --template")
	f.StringVar(&a.inv.Render.TemplateFile, "template-file", "", "Read a Go template from this path")
	f.StringVar(&a.inv.Render.OutputFile, "output-file", "", "Write results to this path; - means stdout")
	f.BoolVarP(&a.inv.Yes, "yes", "y", false, "Answer yes to confirmation prompts")
	f.BoolVar(&a.inv.Render.Summary, "summary", true, "Show analysis summary")
	f.BoolVar(&a.inv.Render.Oneline, "oneline", false, "Use legacy one-line diagnostics")
	f.BoolVarP(&a.inv.Check.Verbose, "verbose", "v", false, "Use legacy progress logging")
	f.BoolVar(&a.inv.Check.Debug, "debug", false, "Use legacy debug logging")
	f.StringVar(&a.opts.logLevel, "log-level", "none", "Log level: none, info or debug")
	if modern {
		f.StringVar(&a.opts.colorMode, "color", "auto", "Color mode: auto, always or never")
		f.Lookup("color").NoOptDefVal = "always"
	} else {
		f.BoolVar(&a.opts.color, "color", false, "Always use color in text diagnostics and help")
	}
	f.BoolVar(&a.opts.noColor, "no-color", false, "Disable color")
	f.BoolVar(&a.opts.helpLegacy, "help-legacy", false, "Show flag equivalents and argument-parsing rules")
	f.BoolP("help", "h", false, "Show help")
	annotateFlag(f, "stdin-filename", "Input")
	for _, name := range []string{"ignore-regex", "ignore"} {
		annotateFlag(f, name, "Filtering")
	}
	for _, name := range []string{"output-format", "output", "template", "format", "template-file", "output-file", "diagnostic-width", "summary", "oneline", "verbose", "debug", "log-level", "color", "no-color"} {
		annotateFlag(f, name, "Output")
	}
	for _, name := range []string{"yes", "help", "help-legacy"} {
		annotateFlag(f, name, "Information")
	}
	for _, name := range []string{"ignore", "output", "format", "oneline", "verbose", "debug", "no-color"} {
		_ = f.MarkHidden(name)
	}
	if !modern {
		f.StringVar(&a.opts.command, "command", "", "Select a command even when a file has the same name")
		_ = f.MarkHidden("command")
		f.BoolVarP(&a.opts.version, "version", "V", false, "Show version and build information")
		annotateFlag(f, "version", "Information")
		f.BoolVar(&a.opts.initConfig, "init-config", false, "Create the repository config")
		f.BoolVar(&a.opts.initConfig, "init", false, "Alias for --init-config")
		f.Var(&a.opts.completion, "completion", "Print generated completions for a shell")
		f.Var(&a.opts.completion, "completions", "Alias for --completion")
		for _, name := range []string{"init", "init-config", "completion", "completions"} {
			annotateFlag(f, name, "Information")
			_ = f.MarkHidden(name)
		}
	}
	choices := func(name string, values []string) {
		f.Lookup(name).Annotations[commandChoicesAnnotation] = values
		if err := c.RegisterFlagCompletionFunc(name, cobra.FixedCompletions(values, cobra.ShellCompDirectiveNoFileComp)); err != nil {
			panic(err)
		}
	}
	choices("output-format", []string{"text", "pretty", "oneline", "json", "jsonl", "sarif", "github"})
	choices("output", []string{"text", "pretty", "oneline", "json", "jsonl", "sarif", "github"})
	choices("log-level", []string{"none", "info", "debug"})
	if modern {
		choices("color", []string{"auto", "always", "never"})
	} else {
		choices("completion", append(completionShellNameList(), "pwsh", "auto"))
		choices("completions", append(completionShellNameList(), "pwsh", "auto"))
	}
	for _, name := range []string{"config", "config-file"} {
		if err := c.MarkFlagFilename(name, "yaml", "yml"); err != nil {
			panic(err)
		}
	}
	for _, name := range []string{"stdin-filename", "output-file", "template-file"} {
		if err := c.MarkFlagFilename(name); err != nil {
			panic(err)
		}
	}
	for _, name := range []string{"template", "format", "ignore", "ignore-regex"} {
		if err := c.RegisterFlagCompletionFunc(name, cobra.NoFileCompletions); err != nil {
			panic(err)
		}
	}
}

func (a *commandApp) capture(op operation) func(*cobra.Command, []string) error {
	return func(c *cobra.Command, args []string) error {
		a.inv.Operation = op
		a.inv.Check.PreCommit = c.Name() == "pre-commit"
		if !a.inv.Check.PreCommit {
			a.inv.Check.Paths = args
		}
		check := c.Name() == "check" || c.Name() == "fix" || a.inv.Check.PreCommit
		c.Flags().Visit(func(f *pflag.Flag) { a.set[f.Name] = true })
		if check && (c.Flags().Changed("ignore-regex") || c.Flags().Changed("ignore")) {
			a.inv.Check.IgnoreRegex = append(a.prefixIgnore, a.inv.Check.IgnoreRegex...)
		}
		if check && c.Flags().Changed("color") {
			a.set["modern-color"] = true
		}
		if a.opts.helpLegacy {
			a.legacyHelp(c)
		}
		return nil
	}
}

func (a *commandApp) jsonOutput() bool {
	return a.inv.JSON || a.opts.output == "json" || a.opts.output == "jsonl" || a.prettyJSONSelected()
}

func (a *commandApp) formatSelected() bool {
	return a.anySet("json", "output-format", "output", "o", "template", "format", "f", "template-file", "oneline")
}

func (a *commandApp) prettyJSONSelected() bool {
	return a.set["json-pretty"] && a.inv.Render.PrettyJSON && !a.formatSelected()
}
