package cli

import (
	"context"
	"fmt"
	"os"

	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/lsp"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/execprotocol"
	"actionlint.kjanat.dev/v2/internal/model/services"
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
	plugins, err := c.pluginSelection(streams.Acquisition).Resolve(ctx, cwd, inputmodel.Request{Kind: inputmodel.Repository})
	if err != nil {
		return 0, err
	}
	runners, err := c.runners()
	if err != nil {
		return 0, err
	}
	service, err := services.FromGitHubServerURL(os.Getenv("GITHUB_SERVER_URL"))
	if err != nil {
		return 0, err
	}
	var provider references.Resolver
	if c.ResolverCommand != "" {
		provider = execprotocol.NewProcess(c.ResolverCommand, c.ResolverArguments, 0)
	}
	metadata, err := c.actionMetadata(streams.Acquisition)
	if err != nil {
		return 0, err
	}
	options := checking.Options{
		RemoteActions: metadata, AllowConfigProcesses: c.AllowConfigProcesses, Runners: runners, Resolver: provider, SuggestCommitPins: c.ResolverCommand != "", WorkingDir: cwd, ConfigFile: c.Config.Path, SkipProjectConfig: c.Config.Disabled || c.Config.Path != "", LogWriter: streams.Stderr, Tools: registry.Registrations(c.Tools),
		EnforcePolicies:   c.EnforcePolicies,
		ConfigConnections: streams.Acquisition.Connections,
		Service:           service,
	}
	return 0, lsp.LanguageServer(ctx, streams.Stdin, streams.Stdout, options, plugins)
}
