package cli

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/localapp"
	"actionlint.kjanat.dev/v2/internal/pluginref"
	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifact"
	"actionlint.kjanat.dev/v2/internal/processconfig"
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
	parent := &cobra.Command{
		Use: "plugin", Short: "Discover, install and inspect WebAssembly plugins", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error { a.help(c, args); return nil },
	}
	a.commonFlags(parent, parent.PersistentFlags())
	a.configFlags(parent.PersistentFlags())
	parent.AddCommand(&cobra.Command{Use: "inspect <path-or-uri>", Short: "Read metadata without executing the plugin", Args: cobra.ExactArgs(1), RunE: a.capture(operationPluginInspect)})
	for _, item := range []struct {
		use, description string
		op               operation
		args             cobra.PositionalArgs
	}{
		{"search [query]", "Search the published plugin catalogue", operationPluginSearch, cobra.MaximumNArgs(1)},
		{"versions <plugin>", "List published tags and host-specific compatibility evidence", operationPluginVersions, cobra.ExactArgs(1)},
		{"install [plugin]", "Install declared plugins, or add a plugin to configuration and lock it", operationPluginInstall, cobra.MaximumNArgs(1)},
		{"list", "List declared and locked plugins without network access", operationPluginList, cobra.NoArgs},
		{"update [plugin]", "Resolve declared versions and update their pins", operationPluginUpdate, cobra.MaximumNArgs(1)},
		{"remove <plugin>", "Remove a plugin from configuration and lockfile", operationPluginRemove, cobra.ExactArgs(1)},
		{"sync", "Refresh release status and restore exact locked artifacts", operationPluginSync, cobra.NoArgs},
	} {
		c := &cobra.Command{Use: item.use, Short: item.description, Args: item.args, RunE: a.capture(item.op)}
		if item.op == operationPluginSearch || item.op == operationPluginVersions || item.op == operationPluginInstall || item.op == operationPluginUpdate || item.op == operationPluginRemove {
			c.Flags().StringVar(&a.inv.Plugin.Registry, "registry", "", "HTTPS catalogue or local candidate file URI (default "+pluginregistry.DefaultURL+")")
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
	// A tool disabled by --no-tool is not run by plugins either.
	return localapp.SelectionWithTools(localapp.PluginSelection{
		Paths: c.Plugins, LockFile: c.PluginLock, ConfigFile: c.Config.Path, Disabled: c.Config.Disabled,
	}, c.DisabledTools, c.NamedTools)
}

// defaultPlugins lets every bundled tool that runs its default command
// receive a default plugin declaration.
func (c checkInvocation) defaultPlugins(warning func(string)) localapp.DefaultPluginOptions {
	options := localapp.DefaultPluginOptions{Warning: warning}
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

type pluginRegistryRequest struct {
	Operation operation
	Options   pluginInvocation
	Paths     []string
	LockFile  string
	Config    repoconfig.Selection
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
	if r.Config.Disabled {
		return 0, commandUsageError{errors.New("plugin management requires configuration; remove --no-config")}
	}
	if name == "" && (r.Options.Version != "" || r.Options.Registry != "") {
		return 0, commandUsageError{errors.New("--version and --registry require a plugin name")}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return 0, err
	}
	project, err := localapp.OpenPluginProject(ctx, cwd, r.LockFile, r.Config.Path)
	if err != nil {
		return 0, err
	}
	project.StatusOptions.Warning = func(message string) {
		_, _ = fmt.Fprintln(streams.Stderr, "warning: "+message)
	}
	var selected pluginregistry.Lock
	switch r.Operation {
	case operationPluginInstall:
		if err := r.install(ctx, project, name); err != nil {
			return 0, err
		}
		selected = project.Lock.Lock
	case operationPluginUpdate:
		if err := r.update(ctx, project, name); err != nil {
			return 0, err
		}
		selected = project.Lock.Lock
	case operationPluginRemove:
		if err := removePlugin(ctx, project, name, r.Options.Registry); err != nil {
			return 0, err
		}
		selected = project.Lock.Lock
	case operationPluginSync:
		selected, err = project.SelectedLock()
		if err != nil {
			return 0, err
		}
		if err := selected.RefreshStatuses(ctx, project.StatusOptions); err != nil {
			return 0, err
		}
		if err := localapp.SyncPlugins(ctx, selected); err != nil {
			return 0, err
		}
		if err := project.SyncDirect(ctx); err != nil {
			return 0, err
		}
	case operationPluginList:
		selected, err = project.SelectedLock()
		if err != nil {
			return 0, err
		}
	case operationCheck, operationVersion, operationRules, operationPluginInspect, operationPluginSearch, operationPluginVersions,
		operationDoctor, operationConfigPath, operationConfigShow, operationConfigValidate, operationConfigMigrate, operationConfigInit, operationCompletion:
		// Search and versions return before project setup; the rest are not plugin lock operations.
		fallthrough
	default:
		return 0, fmt.Errorf("unsupported plugin operation %q", r.Operation)
	}
	if r.JSON {
		return 0, writeCommandJSON(streams.Stdout, struct {
			pluginregistry.Lock
			Direct []string `json:"direct,omitempty"`
		}{selected, project.DirectSources()})
	}
	for _, p := range selected.Plugins {
		if _, err := fmt.Fprintf(streams.Stdout, "%s@%s (%s) sha256:%s\n", p.Name, p.Version, p.Selector, p.SHA256); err != nil {
			return 0, err
		}
	}
	for _, source := range project.DirectSources() {
		if _, err := fmt.Fprintln(streams.Stdout, source); err != nil {
			return 0, err
		}
	}
	if len(selected.Plugins) == 0 && len(project.DirectSources()) == 0 {
		_, err = fmt.Fprintln(streams.Stdout, "No plugins installed.")
	}
	return 0, err
}

func requestedPlugin(name string, options pluginInvocation, existing []pluginref.Reference) (pluginref.Reference, error) {
	ref, err := pluginref.Parse(name)
	if err != nil {
		return pluginref.Reference{}, commandUsageError{err}
	}
	if ref.DirectSource() != "" {
		if options.Version != "" || options.Registry != "" {
			return pluginref.Reference{}, commandUsageError{errors.New("direct plugin URLs cannot use --version or --registry; change the URL and SHA256 pin instead")}
		}
		return ref, nil
	}
	if strings.Contains(name, "@") && options.Version != "" && ref.Version != options.Version {
		return pluginref.Reference{}, commandUsageError{errors.New("plugin @version conflicts with --version")}
	}
	index, err := requestedPluginIndex(ref, options.Registry, existing)
	if err != nil {
		return pluginref.Reference{}, err
	}
	if index >= 0 {
		ref.Registry = existing[index].Registry
		if !strings.Contains(name, "@") {
			ref.Version = existing[index].Version
		}
	}
	if options.Version != "" {
		ref.Version = options.Version
	}
	if options.Registry != "" {
		ref.Registry = options.Registry
	}
	ref, err = pluginref.New(ref.Name, ref.Version, ref.Registry)
	if err != nil {
		return pluginref.Reference{}, commandUsageError{err}
	}
	return ref, nil
}

// A canonical repository can relocate its single existing declaration.
// Aliases stay scoped to the registry that assigns them.
func requestedPluginIndex(ref pluginref.Reference, registry string, existing []pluginref.Reference) (int, error) {
	index, named, count := -1, -1, 0
	for i, old := range existing {
		if old.Name != ref.Name {
			continue
		}
		named, count = i, count+1
		if registry != "" && !pluginregistry.SameRegistry(old.Registry, registry) {
			continue
		}
		if index >= 0 {
			return -1, commandUsageError{fmt.Errorf("plugin %q is declared in multiple registries; select --registry", ref.Name)}
		}
		index = i
	}
	if index < 0 && registry != "" && !ref.IsAlias() {
		if count > 1 {
			return -1, commandUsageError{fmt.Errorf("plugin %q is declared in multiple registries; select an existing --registry", ref.Name)}
		}
		index = named
	}
	return index, nil
}

func (r pluginRegistryRequest) install(ctx context.Context, project *localapp.PluginProject, name string) error {
	refs, err := project.Dependencies(name == "")
	if err != nil {
		return err
	}
	if name != "" {
		// The declaration records the digest an oci:// tag names now.
		name, err = localapp.PinPluginSource(ctx, name)
		if err != nil {
			return err
		}
		ref, err := requestedPlugin(name, r.Options, refs)
		if err != nil {
			return err
		}
		index, err := requestedPluginIndex(ref, r.Options.Registry, refs)
		if err != nil {
			return err
		}
		if index < 0 {
			refs = append(refs, ref)
		} else {
			refs[index] = ref
		}
	}
	if len(refs) == 0 && len(project.Lock.Lock.Plugins) == 0 && len(project.Lock.Lock.Sources) == 0 {
		return nil
	}
	if err := project.Resolve(ctx, refs, nil); err != nil {
		return err
	}
	return project.Save(ctx, refs)
}

func (r pluginRegistryRequest) update(ctx context.Context, project *localapp.PluginProject, name string) error {
	refs, err := project.Dependencies(false)
	if err != nil {
		return err
	}
	update := refs
	if name != "" {
		index, err := declaredPlugin(project, refs, name, r.Options.Registry)
		if err != nil {
			return err
		}
		requested, err := requestedPlugin(name, r.Options, nil)
		if err != nil {
			return err
		}
		ref := refs[index]
		if r.Options.Version != "" || strings.Contains(name, "@") {
			ref.Version = requested.Version
		}
		refs[index] = ref
		update = []pluginref.Reference{ref}
	}
	if len(refs) == 0 && len(project.Lock.Lock.Plugins) == 0 && len(project.Lock.Lock.Sources) == 0 {
		return nil
	}
	if err := project.Resolve(ctx, refs, update); err != nil {
		return err
	}
	return project.Save(ctx, refs)
}

func removePlugin(ctx context.Context, project *localapp.PluginProject, name, registry string) error {
	refs, err := project.Dependencies(false)
	if err != nil {
		return err
	}
	index, err := declaredPlugin(project, refs, name, registry)
	if err != nil {
		return err
	}
	ref := refs[index]
	refs = slices.Delete(refs, index, index+1)
	project.Lock.Lock.Plugins = slices.DeleteFunc(project.Lock.Lock.Plugins, func(pin pluginregistry.Pin) bool {
		return pin.Matches(ref)
	})
	project.Lock.Lock.Sources = slices.DeleteFunc(project.Lock.Lock.Sources, func(pin pluginregistry.SourcePin) bool {
		return ref.DirectSource() != "" && pin.Source() == ref.DirectSource()
	})
	return project.Save(ctx, refs)
}

// declaredPlugin resolves command spelling against declarations and recorded
// pins only. A shorthand from another registry never selects this declaration.
func declaredPlugin(project *localapp.PluginProject, refs []pluginref.Reference, name, registry string) (int, error) {
	requested, err := pluginref.Parse(name)
	if err != nil {
		return -1, commandUsageError{err}
	}
	index := -1
	for i, ref := range refs {
		if registry != "" && !pluginregistry.SameRegistry(ref.Registry, registry) {
			continue
		}
		matches := requested.Name == ref.Name
		for _, pin := range project.Lock.Lock.Plugins {
			if pin.Matches(ref) && (strings.EqualFold(pin.Name, requested.Name) || pin.Alias != "" && pin.Alias == requested.Name) {
				matches = true
			}
		}
		if !matches {
			continue
		}
		if index >= 0 {
			return -1, fmt.Errorf("plugin %q is declared in multiple registries; select --registry", name)
		}
		index = i
	}
	if index < 0 {
		return -1, fmt.Errorf("plugin %q is not declared", name)
	}
	return index, nil
}

func (r pluginRegistryRequest) discover(ctx context.Context, streams Command, registry, name string) error {
	client, err := pluginregistry.New(registry)
	if err != nil {
		return commandUsageError{err}
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
			if _, err := fmt.Fprintf(streams.Stdout, "%s\t%s\t%s\n", p.Name, p.PluginID, strings.Join(p.Aliases, ", ")); err != nil {
				return err
			}
		}
		if len(plugins) == 0 {
			_, err = fmt.Fprintln(streams.Stdout, "No matching plugins published.")
		}
		return err
	}
	ref, err := pluginref.Parse(name)
	if err != nil {
		return commandUsageError{err}
	}
	versions, err := client.Versions(ctx, ref.Name)
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

func selectedRulePlugins(ctx context.Context, selection localapp.PluginSelection) (localapp.PluginSources, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return localapp.PluginSources{}, err
	}
	sources, err := selection.ResolveForRules(ctx, cwd)
	return sources, err
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
	if info.Manifest != "" {
		if _, err := fmt.Fprintf(streams.Stdout, "Platform: %s\nImage manifest: sha256:%s\n", info.Platform, info.Manifest); err != nil {
			return 0, err
		}
	}
	if info.Kind == pluginartifact.ProviderManifest {
		if _, err := fmt.Fprintln(streams.Stdout, "Tool provider manifest:"); err != nil {
			return 0, err
		}
		return 0, writeCommandJSON(streams.Stdout, info.Metadata)
	}
	if info.Metadata == nil {
		_, err := fmt.Fprintln(streams.Stdout, "No actionlint.plugin custom section.")
		return 0, err
	}
	return 0, writeCommandJSON(streams.Stdout, info.Metadata)
}
