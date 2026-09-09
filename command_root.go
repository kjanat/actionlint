package actionlint

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type commandOptions struct {
	linter     LinterOptions
	completion completionShell
	output     string
	initConfig bool
	color      bool
	noColor    bool
	version    bool
	json       bool
	quiet      bool
}

type commandApp struct {
	root      *cobra.Command
	streams   Command
	opts      commandOptions
	status    int
	errorJSON bool
}

type commandUsageError struct{ error }

func newCommandApp(streams *Command) *commandApp {
	a := &commandApp{streams: *streams}
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
			"  actionlint .github/workflows/ci.yml",
			"  actionlint --json .github/workflows/ci.yml",
			"  actionlint --stdin-filename ci.yml -",
			"  actionlint --completion powershell",
		}, "\n"),
		Args:                  cobra.ArbitraryArgs,
		SilenceErrors:         true,
		SilenceUsage:          true,
		DisableFlagsInUseLine: true,
		CompletionOptions:     cobra.CompletionOptions{DisableDefaultCmd: true},
		ValidArgsFunction:     cobra.FixedCompletions([]string{"yaml", "yml"}, cobra.ShellCompDirectiveFilterFileExt),
		RunE:                  a.run,
	}
	a.root.SetIn(a.streams.Stdin)
	a.root.SetOut(a.streams.Stdout)
	a.root.SetErr(a.streams.Stderr)
	a.root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return commandUsageError{err} })
	a.root.SetHelpFunc(a.help)
	a.registerFlags()
	return a
}

const commandGroupAnnotation = "actionlint/group"
const commandChoicesAnnotation = "actionlint/choices"

func (a *commandApp) registerFlags() {
	f, o := a.root.Flags(), &a.opts
	f.SetInterspersed(false)
	f.SetNormalizeFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		if name == "completions" {
			name = "completion"
		}
		return pflag.NormalizedName(name)
	})
	group := func(name, section string) {
		f.Lookup(name).Annotations = map[string][]string{commandGroupAnnotation: {section}}
	}
	boolean := func(target *bool, name, short, description, section string) {
		f.BoolVarP(target, name, short, false, description)
		group(name, section)
	}
	text := func(target *string, name, short, value, description, section string) {
		f.StringVarP(target, name, short, value, description)
		group(name, section)
	}
	text(&o.linter.ConfigFile, "config-file", "", "", "Read configuration from this `path`", "Input")
	text(&o.linter.StdinFileName, "stdin-filename", "", "<stdin>", "Use this `filename` for stdin diagnostics and project detection", "Input")
	boolean(&o.initConfig, "init-config", "", "Create .github/actionlint.yaml with its editor schema directive", "Input")
	f.StringArrayVar(&o.linter.IgnorePatterns, "ignore", nil, "Ignore findings matching this `regexp`; repeat for additional patterns")
	group("ignore", "Input")
	text(&o.output, "output", "o", "text", "Output `mode`: text, oneline, json, jsonl or sarif", "Output")
	boolean(&o.json, "json", "", "Write JSON diagnostics; combine with --help or --version for JSON metadata", "Output")
	text(&o.linter.Format, "format", "f", "", "Render diagnostics with a Go `template`", "Output")
	boolean(&o.linter.Oneline, "oneline", "", "Print one line per finding, without source excerpts", "Output")
	boolean(&o.color, "color", "", "Always use color in text diagnostics", "Output")
	boolean(&o.noColor, "no-color", "", "Disable color; takes precedence over --color", "Output")
	boolean(&o.quiet, "quiet", "q", "Suppress progress and debug logs; keep findings and errors", "Output")
	boolean(&o.linter.Verbose, "verbose", "v", "Write progress information to stderr", "Output")
	boolean(&o.linter.Debug, "debug", "", "Write debug information to stderr", "Output")
	text(&o.linter.Shellcheck, "shellcheck", "", "shellcheck", "ShellCheck `command`, executable path or command line; empty disables it", "External linters")
	text(&o.linter.Pyflakes, "pyflakes", "", "pyflakes", "Pyflakes `command`, executable path or command line; empty disables it", "External linters")
	boolean(&o.version, "version", "", "Print version and build information", "Information")
	f.Var(&o.completion, "completion", "Print generated completions for a `shell`; accepts shell paths and auto")
	group("completion", "Information")
	f.BoolP("help", "h", false, "Show help; combine with --json for the command definition")
	group("help", "Information")
	choices := func(name string, values []string) {
		f.Lookup(name).Annotations[commandChoicesAnnotation] = values
		if err := a.root.RegisterFlagCompletionFunc(name, cobra.FixedCompletions(values, cobra.ShellCompDirectiveNoFileComp)); err != nil {
			panic(err)
		}
	}
	choices("output", []string{"text", "oneline", "json", "jsonl", "sarif"})
	choices("completion", append(completionShellNameList(), "pwsh", "auto"))
	if err := a.root.MarkFlagFilename("config-file", "yaml", "yml"); err != nil {
		panic(err)
	}
	if err := a.root.MarkFlagFilename("stdin-filename"); err != nil {
		panic(err)
	}
	for _, name := range []string{"format", "ignore"} {
		if err := a.root.RegisterFlagCompletionFunc(name, cobra.NoFileCompletions); err != nil {
			panic(err)
		}
	}
}

func (a *commandApp) run(c *cobra.Command, args []string) error {
	o := &a.opts
	if o.version {
		return a.writeVersion()
	}
	if o.completion != "" {
		return writeCompletion(a.streams.Stdout, o.completion, c)
	}
	if err := a.resolveOutput(); err != nil {
		return commandUsageError{err}
	}
	if err := c.Context().Err(); err != nil {
		return err
	}
	options := o.linter
	options.Context = c.Context()
	options.LogWriter = a.streams.Stderr
	if a.jsonOutput() {
		options.LogWriter = &commandJSONLogWriter{out: a.streams.Stderr}
	}
	if o.quiet {
		options.Verbose, options.Debug = false, false
	}
	if o.color {
		options.Color = ColorOptionKindAlways
	}
	if o.noColor {
		options.Color = ColorOptionKindNever
	}
	l, err := NewLinter(a.streams.Stdout, &options)
	if err != nil {
		return err
	}
	if o.initConfig {
		if a.jsonOutput() {
			path, err := l.generateDefaultConfig("")
			if err != nil {
				return err
			}
			return writeCommandJSON(a.streams.Stdout, struct {
				Path string `json:"path"`
			}{path})
		}
		return l.GenerateDefaultConfig("")
	}
	var findings []*Error
	switch {
	case len(args) == 0:
		findings, err = l.LintRepository("")
	case len(args) == 1 && args[0] == "-":
		findings, err = l.LintStdin(a.streams.Stdin)
	default:
		findings, err = l.LintFiles(args, nil)
	}
	if len(findings) > 0 {
		a.status = ExitStatusSuccessProblemFound
	}
	if err == nil {
		err = c.Context().Err()
	}
	return err
}

func (a *commandApp) jsonOutput() bool {
	return a.opts.json || a.opts.output == "json" || a.opts.output == "jsonl"
}

func (a *commandApp) resolveOutput() error {
	o := &a.opts
	if o.json {
		if a.root.Flags().Changed("output") && o.output != "json" {
			return errors.New("--json cannot be combined with a different --output mode")
		}
		o.output = "json"
	}
	if o.linter.Format != "" && (o.json || a.root.Flags().Changed("output")) {
		return errors.New("--format cannot be combined with --json or --output")
	}
	switch o.output {
	case "text":
	case "oneline":
		o.linter.Oneline = true
	case "json", "jsonl":
		o.linter.OutputFormat = OutputFormat(o.output)
	case "sarif":
		o.linter.Format = SARIFTemplate()
	default:
		return fmt.Errorf("invalid output mode %q: choose text, oneline, json, jsonl or sarif", o.output)
	}
	return nil
}

func (a *commandApp) reportError(err error) int {
	status := ExitStatusFailure
	if _, ok := errors.AsType[commandUsageError](err); ok {
		status = ExitStatusInvalidCommandOption
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
