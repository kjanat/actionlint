package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"actionlint.kjanat.dev"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type commandOptions struct {
	output, colorMode, logLevel                     string
	color, noColor, initConfig, version, helpLegacy bool
	completion                                      completionShell
}

type commandApp struct {
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

func newCommandApp(streams *Command) *commandApp {
	a := &commandApp{streams: *streams, inv: defaultInvocation(), set: map[string]bool{}}
	if a.streams.Stdin == nil {
		a.streams.Stdin = strings.NewReader("")
	}
	if a.streams.Stdout == nil {
		a.streams.Stdout = io.Discard
	}
	if a.streams.Stderr == nil {
		a.streams.Stderr = io.Discard
	}
	a.root = &cobra.Command{
		Use:   "actionlint [flags] [files...] [-]",
		Short: "Check GitHub Actions workflows",
		Long:  "Check workflow syntax, expressions, actions and embedded scripts.\nWith no files, discover workflows in the current repository. Pass - alone to read stdin.",
		Example: strings.Join([]string{
			"  actionlint",
			"  actionlint check --output-format=json workflow.yml",
			"  actionlint config show --origin",
			"  actionlint completion powershell",
		}, "\n"),
		SilenceErrors: true, SilenceUsage: true, DisableFlagsInUseLine: true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		RunE:              a.capture("check"),
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: cobra.FixedCompletions([]string{"yaml", "yml"}, cobra.ShellCompDirectiveFilterFileExt),
	}
	a.root.SetIn(a.streams.Stdin)
	a.root.SetOut(a.streams.Stdout)
	a.root.SetErr(a.streams.Stderr)
	a.root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return commandUsageError{err} })
	a.root.SetHelpFunc(a.help)
	a.checkFlags(a.root, false)
	a.root.Flags().SetInterspersed(false)
	check := &cobra.Command{
		Use: "check [flags] [files...] [-]", Short: "Check workflows",
		Long: "Check explicit workflow files, stdin, or the current repository.\nOptions may appear before or after filenames. Directory inputs are not expanded.",
		Args: cobra.ArbitraryArgs, RunE: a.capture("check"), ValidArgsFunction: a.root.ValidArgsFunction,
	}
	a.checkFlags(check, true)
	config := &cobra.Command{
		Use: "config", Short: "Inspect and validate configuration",
		Long: "Select one explicit configuration file or the repository's .github/actionlint.yaml or .yml.\nNo global configuration, merging or environment overrides are applied.",
	}
	a.configFlags(config.PersistentFlags())
	a.commonFlags(config.PersistentFlags())
	for _, item := range []struct{ name, description string }{
		{"init", "Create the repository config with its editor schema directive"},
		{"path", "Print the selected configuration path"},
		{"show", "Show effective configuration settings"},
		{"validate", "Validate the selected configuration using actionlint's loader"},
	} {
		c := &cobra.Command{Use: item.name, Short: item.description, Args: cobra.NoArgs, RunE: a.capture(operation("config " + item.name))}
		if item.name == "show" {
			c.Flags().BoolVar(&a.inv.Origin, "origin", false, "Include the source of each setting")
			annotateFlag(c.Flags(), "origin", "Input")
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
	a.commonFlags(rules.Flags())
	doctor := &cobra.Command{Use: "doctor", Short: "Inspect configuration and external tool availability", Args: cobra.NoArgs, RunE: a.capture("doctor")}
	a.commonFlags(doctor.Flags())
	a.configFlags(doctor.Flags())
	a.toolFlags(doctor.Flags())
	completion := &cobra.Command{
		Use: "completion <shell>", Short: "Generate Bash, Zsh, Fish or PowerShell completions",
		Args: cobra.ExactArgs(1), RunE: a.capture("completion"),
		ValidArgsFunction: cobra.FixedCompletions(append(completionShellNameList(), "pwsh", "auto"), cobra.ShellCompDirectiveNoFileComp),
	}
	version := &cobra.Command{Use: "version", Short: "Show version and build information", Args: cobra.NoArgs, RunE: a.capture("version")}
	a.commonFlags(version.Flags())
	a.root.AddCommand(check, config, rules, doctor, completion, version)
	return a
}

func annotateFlag(f *pflag.FlagSet, name, group string) {
	f.Lookup(name).Annotations = map[string][]string{commandGroupAnnotation: {group}}
}

func (a *commandApp) commonFlags(f *pflag.FlagSet) {
	f.BoolVar(&a.inv.JSON, "json", false, "Write JSON results or metadata")
	f.BoolVarP(&a.inv.Render.Quiet, "quiet", "q", false, "Suppress progress and summaries; keep findings and errors")
	annotateFlag(f, "json", "Output")
	annotateFlag(f, "quiet", "Output")
}

func (a *commandApp) configFlags(f *pflag.FlagSet) {
	f.StringVar(&a.inv.Check.Config.Path, "config", "", "Read configuration from this path")
	f.BoolVar(&a.inv.Check.Config.Disabled, "no-config", false, "Use defaults without reading a configuration file")
	f.StringVar(&a.inv.Check.Config.Path, "config-file", "", "Alias for --config")
	_ = f.MarkHidden("config-file")
	for _, name := range []string{"config", "config-file", "no-config"} {
		annotateFlag(f, name, "Input")
	}
}

func (a *commandApp) toolFlags(f *pflag.FlagSet) {
	f.StringVar(&a.inv.Check.ShellCheck, "shellcheck", "shellcheck", "ShellCheck command or command line; empty disables it")
	f.StringVar(&a.inv.Check.Pyflakes, "pyflakes", "pyflakes", "Pyflakes command or command line; empty disables it")
	annotateFlag(f, "shellcheck", "External linters")
	annotateFlag(f, "pyflakes", "External linters")
}

func (a *commandApp) checkFlags(c *cobra.Command, modern bool) {
	f := c.Flags()
	a.commonFlags(f)
	a.configFlags(f)
	a.toolFlags(f)
	f.StringVar(&a.inv.Check.StdinFilename, "stdin-filename", "<stdin>", "Use this filename for stdin diagnostics and project detection")
	f.StringArrayVar(&a.inv.Check.IgnoreRegex, "ignore-regex", nil, "Ignore findings whose messages match this regexp; repeat as needed")
	f.Var(f.Lookup("ignore-regex").Value, "ignore", "Alias for --ignore-regex")
	f.StringVarP(&a.opts.output, "output-format", "o", "text", "Output mode: text, oneline, json, jsonl, sarif or github")
	f.StringVar(&a.opts.output, "output", "text", "Alias for --output-format")
	f.StringVarP(&a.inv.Render.Template, "template", "f", "", "Render legacy fields with this Go template")
	f.StringVar(&a.inv.Render.Template, "format", "", "Alias for --template")
	f.StringVar(&a.inv.Render.TemplateFile, "template-file", "", "Read a Go template from this path")
	f.StringVar(&a.inv.Render.OutputFile, "output-file", "", "Write results to this path; - means stdout")
	f.BoolVar(&a.inv.Render.Summary, "summary", false, "Write an analysis summary to stderr")
	f.BoolVar(&a.inv.Render.Oneline, "oneline", false, "Use legacy one-line diagnostics")
	f.BoolVarP(&a.inv.Check.Verbose, "verbose", "v", false, "Use legacy progress logging")
	f.BoolVar(&a.inv.Check.Debug, "debug", false, "Use legacy debug logging")
	f.StringVar(&a.opts.logLevel, "log-level", "none", "Log level: none, info or debug")
	if modern {
		f.StringVar(&a.opts.colorMode, "color", "auto", "Color mode: auto, always or never")
		f.Lookup("color").NoOptDefVal = "always"
	} else {
		f.BoolVar(&a.opts.color, "color", false, "Always use color in text diagnostics")
	}
	f.BoolVar(&a.opts.noColor, "no-color", false, "Disable color")
	f.BoolVar(&a.opts.helpLegacy, "help-legacy", false, "Show supported legacy options")
	f.BoolP("help", "h", false, "Show help")
	for _, name := range []string{"stdin-filename", "ignore-regex", "ignore"} {
		annotateFlag(f, name, "Input")
	}
	for _, name := range []string{"output-format", "output", "template", "format", "template-file", "output-file", "summary", "oneline", "verbose", "debug", "log-level", "color", "no-color"} {
		annotateFlag(f, name, "Output")
	}
	for _, name := range []string{"help", "help-legacy"} {
		annotateFlag(f, name, "Information")
	}
	for _, name := range []string{"ignore", "output", "format", "oneline", "verbose", "debug", "no-color"} {
		_ = f.MarkHidden(name)
	}
	if !modern {
		f.BoolVar(&a.opts.version, "version", false, "Show version and build information")
		f.BoolVar(&a.opts.initConfig, "init-config", false, "Create the repository config")
		f.Var(&a.opts.completion, "completion", "Print generated completions for a shell")
		f.Var(&a.opts.completion, "completions", "Alias for --completion")
		for _, name := range []string{"version", "init-config", "completion", "completions"} {
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
	choices("output-format", []string{"text", "oneline", "json", "jsonl", "sarif", "github"})
	choices("output", []string{"text", "oneline", "json", "jsonl", "sarif", "github"})
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
		a.inv.Check.Paths = args
		c.Flags().Visit(func(f *pflag.Flag) { a.set[f.Name] = true })
		if c.Name() == "check" && (c.Flags().Changed("ignore-regex") || c.Flags().Changed("ignore")) {
			a.inv.Check.IgnoreRegex = append(a.prefixIgnore, a.inv.Check.IgnoreRegex...)
		}
		if c.Name() == "check" && c.Flags().Changed("color") {
			a.set["modern-color"] = true
		}
		if a.opts.helpLegacy {
			a.legacyHelp()
		}
		return nil
	}
}

func (a *commandApp) jsonOutput() bool {
	return a.inv.JSON || a.opts.output == "json" || a.opts.output == "jsonl"
}

func (a *commandApp) reportError(err error) int {
	status := actionlint.ExitStatusFailure
	if _, ok := errors.AsType[commandUsageError](err); ok {
		status = actionlint.ExitStatusInvalidCommandOption
	}
	if a.errorJSON || a.jsonOutput() {
		_ = writeCommandJSON(a.streams.Stderr, struct {
			Error    string `json:"error"`
			ExitCode int    `json:"exit_code"`
		}{err.Error(), status})
	} else {
		_, _ = fmt.Fprintln(a.streams.Stderr, err)
	}
	return status
}
