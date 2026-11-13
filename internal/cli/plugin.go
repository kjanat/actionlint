package cli

import (
	"context"
	"maps"
	"os"
	"slices"

	"actionlint.kjanat.dev/v2/internal/checking"
	plugincmd "actionlint.kjanat.dev/v2/internal/cli/plugin"
	processconfig "actionlint.kjanat.dev/v2/internal/integrations/process/config"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type pluginInvocation plugincmd.RegistryOptions

func (a *commandApp) pluginLockFlag(f *pflag.FlagSet) {
	f.StringVar(&a.inv.Check.PluginLock, "plugin-lock", "", "Read a legacy plugin lock `FILE`")
	annotateFlag(f, "plugin-lock", "Rule selection")
}

func (a *commandApp) pluginCommand() *cobra.Command {
	parent := &cobra.Command{
		Use: "plugin", Short: "Discover, install and inspect plugins", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error { a.help(c, args); return nil },
	}
	a.commonFlags(parent, parent.PersistentFlags())
	a.configFlags(parent.PersistentFlags())
	parent.AddCommand(&cobra.Command{Use: "inspect <path-or-uri>", Short: "Read metadata without executing the plugin", Args: cobra.ExactArgs(1), RunE: a.capture(operationPluginInspect)})
	parent.AddCommand(a.pluginAuthoringCommands()...)
	for _, item := range []struct {
		use, description string
		op               operation
		args             cobra.PositionalArgs
	}{
		{"search [query]", "Search the published plugin catalogue", operationPluginSearch, cobra.MaximumNArgs(1)},
		{"versions <plugin>", "List published tags and host-specific compatibility evidence", operationPluginVersions, cobra.ExactArgs(1)},
		{"install [plugin]", "Install plugins and save canonical packages with exact versions", operationPluginInstall, cobra.MaximumNArgs(1)},
		{"list", "List configured exact plugin releases", operationPluginList, cobra.NoArgs},
		{"update [plugin]", "Resolve declared versions and update their pins", operationPluginUpdate, cobra.MaximumNArgs(1)},
		{"remove <plugin>", "Remove a plugin from configuration", operationPluginRemove, cobra.ExactArgs(1)},
		{"sync", "Refresh release status and restore configured artifacts", operationPluginSync, cobra.NoArgs},
	} {
		c := &cobra.Command{Use: item.use, Short: item.description, Args: item.args, RunE: a.capture(item.op)}
		if item.op == operationPluginSearch || item.op == operationPluginVersions || item.op == operationPluginInstall || item.op == operationPluginUpdate || item.op == operationPluginRemove {
			c.Flags().StringVar(&a.inv.Plugin.Registry, "registry", "", "HTTPS catalogue or local candidate file URI (default "+plugininstall.DefaultURL+")")
			annotateFlag(c.Flags(), "registry", "Registry")
		}
		if item.op != operationPluginSearch && item.op != operationPluginVersions {
			a.pluginLockFlag(c.Flags())
		}
		if item.op == operationPluginInstall || item.op == operationPluginUpdate {
			c.Flags().StringVar(&a.inv.Plugin.Version, "version", "", "Exact release tag or latest stable (install default: latest)")
			annotateFlag(c.Flags(), "version", "Registry")
		}
		parent.AddCommand(c)
	}
	return parent
}

func (c checkInvocation) pluginSelection(acquisition plugininstall.Acquisition) plugininstall.Selection {
	// A tool disabled by --no-tool is not run by plugins either.
	return plugininstall.WithTools(plugininstall.Selection{
		RuleSet: checking.ConfigurationRules(),
		Paths:   c.Plugins, LockFile: c.PluginLock, ConfigFile: c.Config.Path, Disabled: c.Config.Disabled,
		Acquisition: acquisition,
	}, c.DisabledTools, c.NamedTools)
}

// defaultPlugins lets every bundled tool that runs its default command
// receive a default plugin declaration.
func (c checkInvocation) defaultPlugins(warning func(string)) plugininstall.DefaultPluginOptions {
	options := plugininstall.DefaultPluginOptions{RuleSet: checking.ConfigurationRules(), Warning: warning}
	for _, id := range slices.Sorted(maps.Keys(c.Tools)) {
		if selection := c.Tools[id]; selection.Command == id && implicitProcessOptions(selection.Process) {
			options.Tools = append(options.Tools, id)
		}
	}
	return options
}

// Optional discovery is a default, not an explicit choice of a native checker.
// Arguments, including those a retired flag adds, still opt into native use.
func implicitProcessOptions(options *processconfig.Options) bool {
	return options == nil || options.Optional && !options.IgnoreConfig && options.Executable == nil &&
		len(options.Arguments) == 0 && len(options.LauncherArguments) == 0 &&
		len(options.Environment) == 0 && options.WorkingDir == ""
}

func selectedRulePlugins(ctx context.Context, selection plugininstall.Selection) (plugininstall.Sources, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return plugininstall.Sources{}, err
	}
	sources, err := selection.ResolveForRules(ctx, cwd)
	return sources, err
}

type (
	pluginRegistryRequest plugincmd.RegistryRequest
	pluginInspectRequest  plugincmd.InspectOptions
)

func (r pluginRegistryRequest) run(ctx context.Context, streams Command) (int, error) {
	return plugincmd.Manage(ctx, streams.commandStreams(), plugincmd.RegistryRequest(r), streams.Acquisition)
}

func (r pluginInspectRequest) run(ctx context.Context, streams Command) (int, error) {
	return plugincmd.Inspect(ctx, streams.commandStreams(), plugincmd.InspectOptions(r), streams.Acquisition)
}
