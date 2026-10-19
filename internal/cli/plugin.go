package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/localapp"
	"actionlint.kjanat.dev/internal/pluginregistry"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type pluginInvocation struct {
	Registry string
	Version  string
}

func (a *commandApp) pluginLockFlag(f *pflag.FlagSet) {
	f.StringVar(&a.inv.Check.PluginLock, "plugin-lock", "", "Use an explicit plugin lock `FILE`")
	annotateFlag(f, "plugin-lock", "Rule selection")
}

func (a *commandApp) pluginCommand() *cobra.Command {
	parent := &cobra.Command{Use: "plugin", Short: "Discover, install and inspect WebAssembly plugins", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error { a.help(c, args); return nil }}
	a.commonFlags(parent, parent.PersistentFlags())
	parent.AddCommand(&cobra.Command{Use: "inspect <path-or-uri>", Short: "Read metadata without executing the plugin", Args: cobra.ExactArgs(1), RunE: a.capture(operationPluginInspect)})
	for _, item := range []struct {
		use, description string
		op               operation
		args             cobra.PositionalArgs
	}{
		{"search [query]", "Search the published plugin catalogue", operationPluginSearch, cobra.MaximumNArgs(1)},
		{"versions <plugin>", "List published tags and host-specific compatibility evidence", operationPluginVersions, cobra.ExactArgs(1)},
		{"install <plugin>", "Download, verify and pin a plugin in the project lock", operationPluginInstall, cobra.ExactArgs(1)},
		{"list", "List locked plugins without network access", operationPluginList, cobra.NoArgs},
		{"update [plugin]", "Resolve saved selectors and atomically update their pins", operationPluginUpdate, cobra.MaximumNArgs(1)},
		{"remove <plugin>", "Remove a plugin from the project lock", operationPluginRemove, cobra.ExactArgs(1)},
		{"sync", "Restore exact locked artifacts without catalogue lookups", operationPluginSync, cobra.NoArgs},
	} {
		c := &cobra.Command{Use: item.use, Short: item.description, Args: item.args, RunE: a.capture(item.op)}
		if item.op == operationPluginSearch || item.op == operationPluginVersions || item.op == operationPluginInstall {
			c.Flags().StringVar(&a.inv.Plugin.Registry, "registry", "", "Registry catalogue URL (default "+pluginregistry.DefaultURL+")")
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

func (c checkInvocation) pluginSelection() localapp.PluginSelection {
	return localapp.PluginSelection{Paths: c.Plugins, LockFile: c.PluginLock, ConfigFile: c.Config.Path, Disabled: c.Config.Disabled}
}

type pluginRegistryRequest struct {
	Operation operation
	Options   pluginInvocation
	Paths     []string
	LockFile  string
	JSON      bool
}

func (r pluginRegistryRequest) run(ctx context.Context, streams Command) (int, error) {
	name := ""
	if len(r.Paths) > 0 {
		name = r.Paths[0]
	}
	registry := r.Options.Registry
	if registry == "" {
		registry = pluginregistry.DefaultURL
	}
	if r.Operation == operationPluginSearch || r.Operation == operationPluginVersions {
		return 0, r.discover(ctx, streams, registry, name)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return 0, err
	}
	f, err := localapp.OpenPluginLock(ctx, cwd, r.LockFile, "")
	if err != nil {
		return 0, err
	}
	switch r.Operation {
	case operationPluginInstall:
		client, err := pluginregistry.New(registry)
		if err != nil {
			return 0, err
		}
		selector := r.Options.Version
		if selector == "" {
			selector = "latest"
		}
		pin, err := client.Resolve(ctx, name, selector)
		if err != nil {
			return 0, err
		}
		if err := f.Set(pin); err != nil {
			return 0, err
		}
		if err := localapp.SyncPlugins(ctx, f.Lock); err != nil {
			return 0, err
		}
		if err := f.Save(ctx); err != nil {
			return 0, err
		}
	case operationPluginUpdate:
		if r.Options.Version != "" && name == "" {
			return 0, commandUsageError{errors.New("--version requires a plugin name for update")}
		}
		if err := updatePluginPins(ctx, f, name, r.Options.Version); err != nil {
			return 0, err
		}
		if err := localapp.SyncPlugins(ctx, f.Lock); err != nil {
			return 0, err
		}
		if err := f.Save(ctx); err != nil {
			return 0, err
		}
	case operationPluginRemove:
		if err := f.Remove(name); err != nil {
			return 0, err
		}
		if err := f.Save(ctx); err != nil {
			return 0, err
		}
	case operationPluginSync:
		if !f.Exists() {
			return 0, fmt.Errorf("plugin lock does not exist: %s", f.Path)
		}
		if err := localapp.SyncPlugins(ctx, f.Lock); err != nil {
			return 0, err
		}
	case operationPluginList:
	default:
		return 0, fmt.Errorf("unsupported plugin operation %q", r.Operation)
	}
	if r.JSON {
		return 0, writeCommandJSON(streams.Stdout, f.Lock)
	}
	for _, p := range f.Lock.Plugins {
		if _, err := fmt.Fprintf(streams.Stdout, "%s@%s (%s) sha256:%s\n", p.Name, p.Version, p.Selector, p.SHA256); err != nil {
			return 0, err
		}
	}
	if len(f.Lock.Plugins) == 0 {
		_, err = fmt.Fprintln(streams.Stdout, "No plugins installed.")
	}
	return 0, err
}

func updatePluginPins(ctx context.Context, f *pluginregistry.LockFile, name, selector string) error {
	if !f.Exists() {
		return fmt.Errorf("plugin lock does not exist: %s", f.Path)
	}
	found := name == ""
	for _, old := range f.Lock.Plugins {
		if name != "" && !strings.EqualFold(name, old.Name) {
			continue
		}
		found = true
		client, err := pluginregistry.New(old.Registry)
		if err != nil {
			return err
		}
		intent := old.Selector
		if selector != "" {
			intent = selector
		}
		pin, err := client.Resolve(ctx, old.Name, intent)
		if err != nil {
			return err
		}
		if err := f.Set(pin); err != nil {
			return err
		}
	}
	if !found {
		return fmt.Errorf("plugin %q is not installed", name)
	}
	return nil
}

func (r pluginRegistryRequest) discover(ctx context.Context, streams Command, registry, name string) error {
	client, err := pluginregistry.New(registry)
	if err != nil {
		return err
	}
	if r.Operation == operationPluginSearch {
		plugins, err := client.Search(ctx, name)
		if err != nil {
			return err
		}
		if r.JSON {
			return writeCommandJSON(streams.Stdout, plugins)
		}
		for _, p := range plugins {
			if _, err := fmt.Fprintf(streams.Stdout, "%s\t%s\n", p.Name, p.PluginID); err != nil {
				return err
			}
		}
		if len(plugins) == 0 {
			_, err = fmt.Fprintln(streams.Stdout, "No matching plugins published.")
		}
		return err
	}
	versions, err := client.Versions(ctx, name)
	if err != nil {
		return err
	}
	if r.JSON {
		return writeCommandJSON(streams.Stdout, versions)
	}
	for _, v := range versions {
		if _, err := fmt.Fprintln(streams.Stdout, v.Version); err != nil {
			return err
		}
		for _, evidence := range v.Compatibility {
			if _, err := fmt.Fprintf(streams.Stdout, "  %q@%q %q: %q\n", evidence.Host.Name, evidence.Host.Revision, evidence.Policy, evidence.Outcome); err != nil {
				return err
			}
		}
	}
	return nil
}

func selectedRulePlugins(ctx context.Context, selection localapp.PluginSelection) ([]string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	sources, err := selection.Resolve(ctx, cwd, application.Input{Kind: application.Repository})
	return sources.Paths, err
}

type pluginInspectRequest struct {
	Source string
	JSON   bool
}

func (r pluginInspectRequest) run(ctx context.Context, streams Command) (int, error) {
	info, err := localapp.InspectPlugin(ctx, r.Source)
	if err != nil {
		return 0, err
	}
	if r.JSON {
		return 0, writeCommandJSON(streams.Stdout, info)
	}
	if _, err := fmt.Fprintf(streams.Stdout, "%s\nKind: %s\nSHA256: %s\nSize: %d bytes (%d decoded)\n", info.Source, info.Kind, info.SHA256, info.Size, info.ModuleSize); err != nil {
		return 0, err
	}
	if info.Metadata == nil {
		_, err := fmt.Fprintln(streams.Stdout, "No actionlint.plugin custom section.")
		return 0, err
	}
	return 0, writeCommandJSON(streams.Stdout, info.Metadata)
}
