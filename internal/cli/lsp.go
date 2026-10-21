package cli

import (
	"context"
	"errors"
	"os"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/localapp"
	"actionlint.kjanat.dev/internal/resolution"
	"actionlint.kjanat.dev/internal/tools/adapters"
	"actionlint.kjanat.dev/resolver"
)

type lspRequest struct{ Check checkInvocation }

func (r lspRequest) run(ctx context.Context, streams Command) (int, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return 0, err
	}
	c := r.Check
	if len(c.Plugins) != 0 && c.PluginLock != "" {
		return 0, commandUsageError{errors.New("--plugin and --plugin-lock cannot be combined")}
	}
	plugins, err := c.pluginSelection().Resolve(ctx, cwd, application.Input{Kind: application.Repository})
	if err != nil {
		return 0, err
	}
	runners, err := c.runners()
	if err != nil {
		return 0, err
	}
	var provider resolver.Resolver
	if c.ResolverCommand != "" {
		provider = &resolution.Process{Executable: c.ResolverCommand, Arguments: c.ResolverArguments}
	}
	return 0, localapp.LanguageServer(ctx, streams.Stdin, streams.Stdout, application.Options{AllowConfigProcesses: c.AllowConfigProcesses, Runners: runners, Resolver: provider, WorkingDir: cwd, ConfigFile: c.Config.Path, SkipProjectConfig: c.Config.Disabled || c.Config.Path != "", LogWriter: streams.Stderr, Tools: adapters.Options{ShellCheck: c.ShellCheck, ShellcheckOptions: c.ShellcheckOptions, Ruff: c.Ruff, RuffOptions: c.RuffOptions}.Registrations()}, plugins)
}
