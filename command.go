package actionlint

import (
	"context"
	"fmt"
	"io"
	"runtime/debug"
	"slices"

	"github.com/spf13/cobra"
)

// These variables might be modified by ldflags on building release binaries by GoReleaser. Do not modify manually.
var (
	version       = ""
	installedFrom = ""
)

const (
	// ExitStatusSuccessNoProblem means linting completed without findings.
	ExitStatusSuccessNoProblem = 0
	// ExitStatusSuccessProblemFound means linting completed with findings.
	ExitStatusSuccessProblemFound = 1
	// ExitStatusInvalidCommandOption means command-line parsing or validation failed.
	ExitStatusInvalidCommandOption = 2
	// ExitStatusFailure means linting could not complete.
	ExitStatusFailure = 3
)

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
		return "unknown"
	}
	return info.Main.Version
}

// Command runs the CLI with caller-provided streams. Each invocation has its own
// argument parser and option state, so a Command can be reused.
type Command struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Main runs the CLI and returns its exit status. Args includes the executable
// name, as in os.Args.
func (cmd *Command) Main(args []string) int {
	return cmd.MainContext(context.Background(), args)
}

// MainContext runs the CLI with a context that also controls external linters.
// Cancellation returns ExitStatusFailure. Args includes the executable name.
func (cmd *Command) MainContext(ctx context.Context, args []string) int {
	app := newCommandApp(cmd)
	if len(args) > 0 {
		args = args[1:]
	}
	if args == nil {
		args = []string{}
	}
	if len(args) > 0 && !commandFileExists(args[0]) && (args[0] == cobra.ShellCompRequestCmd || args[0] == cobra.ShellCompNoDescRequestCmd) {
		app.root.SetErr(io.Discard)
		app.root.SetArgs(normalizeCommandArgs(app.root.Flags(), args))
		if err := app.root.ExecuteContext(ctx); err != nil {
			return app.reportError(commandUsageError{err})
		}
		return app.status
	}
	rest, forced, terminated, err := app.parseLegacy(args)
	if err != nil {
		return app.reportError(err)
	}
	if app.helpShown {
		return app.status
	}
	legacyOperation := app.opts.version || app.opts.initConfig || app.opts.completion != "" || app.opts.helpLegacy
	modern := forced != "" || (!legacyOperation && !terminated && len(rest) > 0 && isCommandName(rest[0]) && !commandFileExists(rest[0]))
	if modern {
		if forced != "" {
			if !isCommandName(forced) {
				return app.reportError(commandUsageError{fmt.Errorf("unknown command %q", forced)})
			}
			rest = append([]string{forced}, rest...)
		}
		app.errorJSON = app.errorJSON || requestsJSON(app.root.Flags(), rest[1:], true)
		app.root.SetArgs(rest)
		app.prefixIgnore = slices.Clone(app.inv.Check.IgnoreRegex)
		if err := app.root.ExecuteContext(ctx); err != nil {
			return app.reportError(commandUsageError{err})
		}
	} else {
		app.inv.Legacy = true
		app.inv.Check.Paths = rest
		switch {
		case app.opts.helpLegacy:
			app.legacyHelp()
		case app.opts.version:
			app.inv.Operation = "version"
		case app.opts.completion != "":
			app.inv.Operation, app.inv.Shell = "completion", string(app.opts.completion)
		case app.opts.initConfig:
			app.inv.Operation = "config init"
		}
	}
	if app.helpShown {
		return app.status
	}
	if err := app.prepareInvocation(); err != nil {
		return app.reportError(err)
	}
	if app.inv.Operation == "completion" {
		var shell completionShell
		if err := shell.Set(app.inv.Shell); err != nil {
			return app.reportError(commandUsageError{err})
		}
		if err := writeCompletion(app.streams.Stdout, shell, app.root); err != nil {
			return app.reportError(err)
		}
		return 0
	}
	status, err := executeInvocation(ctx, app.streams, app.inv)
	if err != nil {
		return app.reportError(err)
	}
	app.status = status
	return app.status
}
