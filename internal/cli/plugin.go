package cli

import (
	"context"
	"maps"
	"os"
	"slices"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/integrations/process"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginselection"

	"github.com/spf13/cobra"
)

func (a *commandApp) pluginCommand() *cobra.Command {
	parent := &cobra.Command{
		Use: "plugin", Short: "Discover, install and inspect plugins", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error { a.help(c, args); return nil },
	}
	a.commonFlags(parent, parent.PersistentFlags())
	a.configFlags(parent.PersistentFlags())
	inspect := &cobra.Command{Use: "inspect <plugin-or-path-or-uri>", Short: "Read metadata without executing the plugin", Args: cobra.ExactArgs(1), RunE: a.capture(operationPluginInspect)}
	inspect.Flags().StringVar(&a.inv.Plugin.Registry, "registry", "", "HTTPS catalogue or local candidate file URI (default "+pluginregistry.DefaultURL+")")
	annotateFlag(inspect.Flags(), "registry", "Registry")
	parent.AddCommand(inspect)
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
			c.Flags().StringVar(&a.inv.Plugin.Registry, "registry", "", "HTTPS catalogue or local candidate file URI (default "+pluginregistry.DefaultURL+")")
			annotateFlag(c.Flags(), "registry", "Registry")
		}
		if item.op == operationPluginInstall || item.op == operationPluginUpdate {
			c.Flags().StringVar(&a.inv.Plugin.Version, "version", "", "Exact release tag or latest stable (install default: latest)")
			annotateFlag(c.Flags(), "version", "Registry")
		}
		parent.AddCommand(c)
	}
	return parent
}

func (c checkInvocation) pluginSelection(acquisition pluginacquisition.Settings) pluginselection.Selection {
	// A tool disabled by --no-tool is not run by plugins either.
	return pluginselection.WithTools(pluginselection.Selection{
		RuleSet: checking.ConfigurationRules(),
		Paths:   c.Plugins, ConfigFile: c.Config.Path, Disabled: c.Config.Disabled,
		Acquisition:  acquisition,
		DefaultTools: c.defaultPlugins(nil).Tools,
		DisableTools: c.NoTools,
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
func implicitProcessOptions(options *process.CommandOptions) bool {
	return options == nil || options.Optional && !options.IgnoreConfig && options.Executable == nil &&
		len(options.Arguments) == 0 && len(options.LauncherArguments) == 0 &&
		len(options.Environment) == 0 && options.WorkingDir == ""
}

func selectedRulePlugins(ctx context.Context, selection pluginselection.Selection) (pluginselection.Sources, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return pluginselection.Sources{}, err
	}
	sources, err := selection.ResolveForRules(ctx, cwd)
	return sources, err
}
