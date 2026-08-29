package actionlint

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"regexp"
	"runtime"
	"runtime/debug"
)

// These variables might be modified by ldflags on building release binaries by GoReleaser. Do not modify manually
var (
	version       = ""
	installedFrom = ""
)

const (
	// ExitStatusSuccessNoProblem is the exit status when the command ran successfully with no problem found.
	ExitStatusSuccessNoProblem = 0
	// ExitStatusSuccessProblemFound is the exit status when the command ran successfully with some problem found.
	ExitStatusSuccessProblemFound = 1
	// ExitStatusInvalidCommandOption is the exit status when parsing command line options failed.
	ExitStatusInvalidCommandOption = 2
	// ExitStatusFailure is the exit status when the command stopped due to some fatal error while checking workflows.
	ExitStatusFailure = 3
)

func printUsageHeader(out io.Writer) {
	v := getCommandVersion()
	b := "HEAD"
	if regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(v) {
		b = "v" + v
	}

	_, _ = fmt.Fprintf(out, `Usage: actionlint [FLAGS] [FILES...] [-]

  actionlint is a linter for GitHub Actions workflow files.

  To check all YAML files in current repository, just run actionlint without
  arguments. It automatically finds the nearest '.github/workflows' directory:

    $ actionlint

  To check specific files, pass the file paths as arguments:

    $ actionlint file1.yaml file2.yaml

  To check content which is not saved in file yet (e.g. output from some
  command), pass - argument. It reads stdin and checks it as workflow file:

    $ actionlint -

  To serialize errors into JSON, use -format option. It allows to format error
  messages flexibly with Go template syntax.

    $ actionlint -format '{{json .}}'

Documents:

  - List of checks: https://github.com/kjanat/actionlint/tree/%s/docs/checks.md
  - Usage:          https://github.com/kjanat/actionlint/tree/%s/docs/usage.md
  - Configuration:  https://github.com/kjanat/actionlint/tree/%s/docs/config.md

Flags:
`, b, b, b)
}

func getInstalledFrom() string {
	if installedFrom != "" {
		return installedFrom
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		for _, s := range info.Settings {
			if s.Key == "vcs" {
				return "from source"
			}
		}
		return "go install"
	}
	return "from source"
}

func getCommandVersion() string {
	if version != "" {
		return version
	}

	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return "unknown" // Reaches only when actionlint package is built outside module
	}

	return info.Main.Version
}

// Command represents entire actionlint command. Given stdin/stdout/stderr are used for input/output.
type Command struct {
	// Stdin is a reader to read input from stdin
	Stdin io.Reader
	// Stdout is a writer to write output to stdout
	Stdout io.Writer
	// Stderr is a writer to write output to stderr
	Stderr io.Writer
}

func (cmd *Command) runLinter(args []string, opts *LinterOptions, initConfig bool) ([]*Error, error) {
	l, err := NewLinter(cmd.Stdout, opts)
	if err != nil {
		return nil, err
	}

	if initConfig {
		return nil, l.GenerateDefaultConfig("")
	}

	if len(args) == 0 {
		return l.LintRepository("")
	}

	if len(args) == 1 && args[0] == "-" {
		return l.LintStdin(cmd.Stdin)
	}

	return l.LintFiles(args, nil)
}

type ignorePatternFlags []string

func (i *ignorePatternFlags) String() string {
	return "option for ignore patterns"
}
func (i *ignorePatternFlags) Set(v string) error {
	*i = append(*i, v)
	return nil
}
func (i *ignorePatternFlags) repeatableFlag() {}

type commandFlags struct {
	opts       LinterOptions
	ignorePats ignorePatternFlags
	initConfig bool
	noColor    bool
	color      bool
	version    bool
	completion completionShell
}

func (f *commandFlags) newFlagSet(name string, out io.Writer) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(out)
	flags.Var(&f.ignorePats, "ignore", "Regular expression matching to error messages you want to ignore. This flag is repeatable")
	flags.StringVar(&f.opts.Shellcheck, "shellcheck", "shellcheck", "Command line of \"shellcheck\" external command. A command name, a file path, or a command with flags such as \"shellcheck -e SC2086\". If empty, shellcheck integration will be disabled")
	flags.StringVar(&f.opts.Pyflakes, "pyflakes", "pyflakes", "Command line of \"pyflakes\" external command. A command name, a file path, or a command with flags such as \"python3 -m pyflakes\" or \"uvx pyflakes\". If empty, pyflakes integration will be disabled")
	flags.BoolVar(&f.opts.Oneline, "oneline", false, "Use one line per one error. Useful for reading error messages from programs")
	flags.StringVar(&f.opts.Format, "format", "", "Custom template to format error messages in Go template syntax. See the usage documentation for more details")
	flags.StringVar(&f.opts.ConfigFile, "config-file", "", "File path to config file")
	flags.BoolVar(&f.initConfig, "init-config", false, "Generate default config file at .github/actionlint.yaml in current project")
	flags.BoolVar(&f.noColor, "no-color", false, "Disable colorful output")
	flags.BoolVar(&f.color, "color", false, "Always enable colorful output. This is useful to force colorful outputs")
	flags.BoolVar(&f.opts.Verbose, "verbose", false, "Enable verbose output")
	flags.BoolVar(&f.opts.Debug, "debug", false, "Enable debug output (for development)")
	flags.BoolVar(&f.version, "version", false, "Show version and how this binary was installed")
	flags.StringVar(&f.opts.StdinFileName, "stdin-filename", "<stdin>", "File name when reading input from stdin")
	flags.Var(&f.completion, "completion", "Print a shell completion script for the given `shell`. One of \"bash\", \"fish\", \"powershell\", \"zsh\". Also accepted are \"pwsh\", a shell path such as \"$SHELL\", and \"auto\" to detect the current shell")
	return flags
}

// Main is main function of actionlint. It takes command line arguments as string slice and returns
// exit status. The args should be entire arguments including the program name, usually given via
// os.Args.
func (cmd *Command) Main(args []string) int {
	var f commandFlags

	flags := f.newFlagSet(args[0], cmd.Stderr)
	flags.Usage = func() {
		printUsageHeader(cmd.Stderr)
		flags.PrintDefaults()
	}
	if err := flags.Parse(completionAliasArgs(args[1:])); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			// When -h or -help
			return ExitStatusSuccessNoProblem
		}
		return ExitStatusInvalidCommandOption
	}

	if f.version {
		name := "actionlint"
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Path != "" {
			name = info.Main.Path
		}
		_, _ = fmt.Fprintf(
			cmd.Stdout,
			"%s %s\n%s\nbuilt with %s compiler for %s/%s\n",
			name,
			getCommandVersion(),
			getInstalledFrom(),
			runtime.Version(),
			runtime.GOOS,
			runtime.GOARCH,
		)
		return ExitStatusSuccessNoProblem
	}

	if f.completion != "" {
		if err := writeCompletion(cmd.Stdout, f.completion, flags); err != nil {
			_, _ = fmt.Fprintln(cmd.Stderr, err.Error())
			return ExitStatusFailure
		}
		return ExitStatusSuccessNoProblem
	}

	f.opts.IgnorePatterns = f.ignorePats
	f.opts.LogWriter = cmd.Stderr

	if f.color {
		f.opts.Color = ColorOptionKindAlways
	}
	if f.noColor {
		f.opts.Color = ColorOptionKindNever
	}

	errs, err := cmd.runLinter(flags.Args(), &f.opts, f.initConfig)
	if err != nil {
		_, _ = fmt.Fprintln(cmd.Stderr, err.Error())
		return ExitStatusFailure
	}
	if len(errs) > 0 {
		return ExitStatusSuccessProblemFound // Linter found some issues, yay!
	}

	return ExitStatusSuccessNoProblem
}
