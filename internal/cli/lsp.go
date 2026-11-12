package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	commandargs "actionlint.kjanat.dev/v2/internal/cli/arguments"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	lsp "actionlint.kjanat.dev/v2/internal/lsp"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	"actionlint.kjanat.dev/v2/internal/input/references"
	resolverprocess "actionlint.kjanat.dev/v2/internal/input/references/process"
)

type lspRequest struct{ Check checkInvocation }

func (r lspRequest) run(ctx context.Context, streams Command) (int, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return 0, err
	}
	c := r.Check
	for _, warning := range c.Deprecations {
		_, _ = fmt.Fprintln(streams.Stderr, "warning: "+warning)
	}
	if len(c.Plugins) != 0 && c.PluginLock != "" {
		return 0, commandargs.UsageError{Cause: errors.New("--plugin and --plugin-lock cannot be combined")}
	}
	plugins, err := c.pluginSelection(streams.Acquisition).Resolve(ctx, cwd, inputmodel.Request{Kind: inputmodel.Repository})
	if err != nil {
		return 0, err
	}
	runners, err := c.runners()
	if err != nil {
		return 0, err
	}
	var provider references.Resolver
	if c.ResolverCommand != "" {
		provider = resolverprocess.NewProcess(c.ResolverCommand, c.ResolverArguments, 0)
	}
	metadata, err := c.actionMetadata(streams.Acquisition)
	if err != nil {
		return 0, err
	}
	return 0, lsp.LanguageServer(ctx, streams.Stdin, streams.Stdout, checking.Options{RemoteActions: metadata, AllowConfigProcesses: c.AllowConfigProcesses, Runners: runners, Resolver: provider, SuggestCommitPins: c.ResolverCommand != "", WorkingDir: cwd, ConfigFile: c.Config.Path, SkipProjectConfig: c.Config.Disabled || c.Config.Path != "", LogWriter: streams.Stderr, Tools: registry.Registrations(c.Tools)}, plugins)
}
