package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"

	commandargs "actionlint.kjanat.dev/v2/internal/cli/arguments"
	commandstreams "actionlint.kjanat.dev/v2/internal/cli/streams"

	configsource "actionlint.kjanat.dev/v2/internal/input/configuration/source"
	"actionlint.kjanat.dev/v2/internal/input/discovery"
	networkaccounting "actionlint.kjanat.dev/v2/internal/integrations/http/accounting"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"

	"github.com/spf13/cobra"
)

// Command runs the CLI with caller-provided streams. Each invocation has its own
// argument parser and option state, so a Command can be reused.
type Command struct {
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	Acquisition plugininstall.Acquisition
}

func (cmd *Command) commandStreams() commandstreams.Streams {
	return commandstreams.Streams{Stdin: cmd.Stdin, Stdout: cmd.Stdout, Stderr: cmd.Stderr}
}

// Main runs the CLI and returns its exit status. Args includes the executable
// name, as in os.Args.
func (cmd *Command) Main(args []string) int {
	return cmd.MainContext(context.Background(), args)
}

// MainContext runs the CLI with a context that also controls external linters.
// Cancellation returns exit status 3. Args includes the executable name.
func (cmd *Command) MainContext(ctx context.Context, args []string) (status int) {
	ctx, accounting := networkaccounting.TrackRequests(ctx)
	app := newCommandApp(ctx, cmd)
	app.root.SetContext(ctx)
	defer func() {
		if app.opts.networkStats {
			entry := struct {
				Network networkaccounting.Statistics `json:"network"`
			}{accounting.Statistics()}
			if err := json.NewEncoder(app.streams.Stderr).Encode(entry); err != nil {
				status = 3
			}
		}
	}()
	defer func() {
		if app.directory.original != "" {
			if err := os.Chdir(app.directory.original); err != nil {
				status = app.reportError(fmt.Errorf("restore working directory: %w", err))
			}
		}
	}()
	// Root parsing must select an operation before errors can describe a check.
	app.inv.Operation = ""
	if len(args) > 0 {
		args = args[1:]
	}
	if len(args) > 0 && (args[0] == cobra.ShellCompRequestCmd || args[0] == cobra.ShellCompNoDescRequestCmd) {
		app.root.SetErr(io.Discard)
		app.root.SetArgs(normalizeCommandArgs(app.root, args))
		if err := app.root.ExecuteContext(ctx); err != nil {
			return app.reportError(commandargs.UsageError{Cause: err})
		}
		return app.status
	}
	rest, terminated, err := app.parseRoot(args)
	if err != nil {
		return app.reportError(err)
	}
	if app.helpShown {
		return app.status
	}
	if len(rest) == 0 && !terminated && (len(app.set) == 0 || len(app.set) == 1 && app.set["directory"]) {
		directory, err := os.Getwd()
		if err != nil {
			return app.reportError(err)
		}
		if discovery.RepositoryRoot(directory) == "" {
			app.root.SetContext(ctx)
			app.streams.Stderr = app.streams.Stdout
			app.help(app.root, nil)
			return app.status
		}
		rest = []string{"check"}
	}
	legacyOperation := app.opts.version || app.opts.initConfig || app.opts.completion != "" || app.opts.helpLegacy
	modern := !legacyOperation && !terminated && len(rest) > 0 && isCommandName(rest[0])
	if modern {
		app.errorJSON = app.errorJSON || requestsJSON(app.root.Flags(), rest[1:], true)
		// Parsing can fail before a command's RunE records its full operation.
		// Keep metadata-command errors out of the analysis-result contract.
		app.inv.Operation = operation(rest[0])
		if rest[0] == "pre-commit" {
			app.inv.Operation = operationCheck
		}
		rest = normalizeCommandArgs(app.root, rest)
		app.root.SetArgs(rest)
		app.prefixIgnore = slices.Clone(app.inv.Check.IgnoreRegex)
		app.prefixIgnoreSelectors = slices.Clone(app.inv.Check.IgnoreSelectors)
		if err := app.root.ExecuteContext(ctx); err != nil {
			return app.reportError(commandargs.UsageError{Cause: err})
		}
	} else {
		app.inv.Legacy = true
		app.inv.Check.Paths = rest
		switch {
		case app.opts.helpLegacy:
			app.legacyHelp(app.root)
		case app.opts.version:
			app.inv.Operation = "version"
		case app.opts.completion != "":
			app.inv.Operation, app.inv.Shell = "completion", string(app.opts.completion)
		case app.opts.initConfig:
			app.inv.Operation = "config init"
		default:
			app.inv.Operation = operationCheck
		}
	}
	if app.helpShown {
		return app.status
	}
	if err := app.applyEnvironment(); err != nil {
		return app.reportError(err)
	}
	if app.inv.Operation == operationCheck {
		ctx = configsource.WithReadCache(ctx)
		app.ctx = ctx
	}
	outputConfigErr := app.applyConfiguredOutput()
	if err := app.prepareInvocation(); err != nil {
		return app.reportError(err)
	}
	if outputConfigErr != nil {
		log := app.streams.Stderr
		if app.inv.JSON {
			log = &commandJSONLogWriter{out: log}
		}
		writeCheckBuildIdentity(log, app.inv.Check, app.inv.Render)
		return app.reportError(outputConfigErr)
	}
	if app.inv.Operation == "completion" {
		if err := ctx.Err(); err != nil {
			return app.reportError(err)
		}
		var shell completionShell
		if err := shell.Set(app.inv.Shell); err != nil {
			return app.reportError(commandargs.UsageError{Cause: err})
		}
		if err := writeCompletion(app.streams.Stdout, shell, app.root); err != nil {
			return app.reportError(err)
		}
		return 0
	}
	status, err = executeInvocation(ctx, app.streams, app.inv)
	if err != nil {
		return app.reportError(err)
	}
	app.status = status
	return app.status
}
