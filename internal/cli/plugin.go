package cli

import (
	"context"
	"maps"
	"os"
	"slices"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/integrations/hostprocess"
	"actionlint.kjanat.dev/v2/internal/plugins/installation"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginselection"

	"github.com/spf13/cobra"
)

func (a *commandApp) pluginCommand() *cobra.Command {
	// Cobra resolves aliases everywhere; help and completion list only names.
	parent := &cobra.Command{
		Use: "plugin", Aliases: []string{"plugins"}, Short: "Discover, install and inspect plugins", Args: cobra.NoArgs,
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
		aliases          []string
		op               operation
		args             cobra.PositionalArgs
	}{
		{"search [query]", "Search the published plugin catalogue", nil, operationPluginSearch, cobra.MaximumNArgs(1)},
		{"versions <plugin>", "List published tags and host-specific compatibility evidence", nil, operationPluginVersions, cobra.ExactArgs(1)},
		{"install [plugin]", "Install plugins and save canonical packages with exact versions", []string{"add"}, operationPluginInstall, cobra.MaximumNArgs(1)},
		{"list", "List configured exact plugin releases", nil, operationPluginList, cobra.NoArgs},
		{"update [plugin]", "Resolve declared versions and update their pins", nil, operationPluginUpdate, cobra.MaximumNArgs(1)},
		{"remove <plugin>", "Remove a plugin from configuration", nil, operationPluginRemove, cobra.ExactArgs(1)},
		{"sync", "Refresh release status and restore configured artifacts", nil, operationPluginSync, cobra.NoArgs},
	} {
		c := &cobra.Command{Use: item.use, Aliases: item.aliases, Short: item.description, Args: item.args, RunE: a.capture(item.op)}
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

func (c checkInvocation) pluginSelection(acquisition dependencycache.Acquisition) pluginselection.PluginSelection {
	return pluginselection.WithTools(pluginselection.PluginSelection{
		RuleSet: checking.ConfigurationRules(),
		Paths:   c.Plugins, ConfigFile: c.Config.Path, Disabled: c.Config.Disabled,
		Acquisition:          acquisition,
		DefaultTools:         c.defaultPlugins(nil).Tools,
		DisableTools:         c.NoTools,
		TrustDefaultRegistry: c.TrustDefaultRegistry,
	}, c.DisabledTools, c.NamedTools)
}

func (c checkInvocation) defaultPlugins(warning func(string)) installation.DefaultPluginOptions {
	options := installation.DefaultPluginOptions{RuleSet: checking.ConfigurationRules(), Warning: warning}
	for _, id := range slices.Sorted(maps.Keys(c.Tools)) {
		if selection := c.Tools[id]; selection.Command == id && implicitProcessOptions(selection.Process) {
			options.Tools = append(options.Tools, id)
		}
	}
	return options
}

// Optional discovery is a default and does not select a native checker explicitly.
// Arguments, including arguments from a retired flag, still opt into native use.
func implicitProcessOptions(options *hostprocess.CommandOptions) bool {
	return options == nil || options.Optional && !options.IgnoreConfig && options.Executable == nil &&
		len(options.Arguments) == 0 && len(options.LauncherArguments) == 0 &&
		len(options.Environment) == 0 && options.WorkingDir == ""
}

func selectedRulePlugins(ctx context.Context, selection pluginselection.PluginSelection) (pluginselection.Sources, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return pluginselection.Sources{}, err
	}
	sources, err := selection.ResolveForRules(ctx, cwd)
	return sources, err
}
