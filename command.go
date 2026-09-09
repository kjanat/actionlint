package actionlint

import (
	"context"
	"io"
	"runtime/debug"

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
	args = normalizeCommandArgs(app.root.Flags(), args)
	if len(args) > 0 && (args[0] == cobra.ShellCompRequestCmd || args[0] == cobra.ShellCompNoDescRequestCmd) {
		app.root.SetErr(io.Discard)
	}
	app.errorJSON = commandRequestsJSON(app.root.Flags(), args)
	app.root.SetArgs(args)
	if err := app.root.ExecuteContext(ctx); err != nil {
		return app.reportError(err)
	}
	return app.status
}
