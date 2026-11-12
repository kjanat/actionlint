package plugincmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking"
	commandargs "actionlint.kjanat.dev/v2/internal/cli/arguments"
	commandstreams "actionlint.kjanat.dev/v2/internal/cli/streams"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
)

type RegistryOptions struct {
	Registry string
	Version  string
}

type undeclaredPluginError struct{ name string }

func (e undeclaredPluginError) Error() string {
	return fmt.Sprintf("plugin %q is not declared", e.name)
}

type Operation string

const (
	Search   Operation = "plugin search"
	Versions Operation = "plugin versions"
	Install  Operation = "plugin install"
	List     Operation = "plugin list"
	Update   Operation = "plugin update"
	Remove   Operation = "plugin remove"
	Sync     Operation = "plugin sync"
)

type RegistryRequest struct {
	Operation Operation
	Options   RegistryOptions
	Paths     []string
	LockFile  string
	Config    repoconfig.Selection
	JSON      bool
}

func Manage(ctx context.Context, streams commandstreams.Streams, r RegistryRequest, acquisition plugininstall.Acquisition) (int, error) {
	name := ""
	if len(r.Paths) > 0 {
		name = r.Paths[0]
	}
	registry := r.Options.Registry
	if registry == "" {
		registry = plugininstall.DefaultURL
	}
	if r.Operation == Search || r.Operation == Versions {
		return 0, r.discover(ctx, streams, registry, name, acquisition)
	}
	if r.Config.Disabled {
		return 0, commandargs.UsageError{Cause: errors.New("plugin management requires configuration; remove --no-config")}
	}
	if name == "" && (r.Options.Version != "" || r.Options.Registry != "") {
		return 0, commandargs.UsageError{Cause: errors.New("--version and --registry require a plugin name")}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return 0, err
	}
	project, err := plugininstall.OpenProject(ctx, checking.ConfigurationRules(), acquisition, cwd, r.LockFile, r.Config.Path)
	if err != nil {
		return 0, err
	}
	project.StatusOptions.Warning = func(message string) {
		_, _ = fmt.Fprintln(streams.Stderr, "warning: "+message)
	}
	// Only an endpoint supplied in this invocation grants private-network
	// authority. Selecting configuration, locks or an operation grants none.
	explicitEndpoint := r.Options.Registry
	if strings.Contains(name, "://") {
		explicitEndpoint = name
	}
	if explicitEndpoint != "" {
		grant, err := pluginartifact.ExplicitSource(explicitEndpoint)
		if err != nil {
			return 0, commandargs.UsageError{Cause: err}
		}
		project.StatusOptions.Network = grant
	}
	var selected plugininstall.Lock
	switch r.Operation {
	case Install:
		if err := r.install(ctx, project, name); err != nil {
			return 0, err
		}
		selected = project.Lock.Lock
	case Update:
		if err := r.update(ctx, project, name); err != nil {
			return 0, err
		}
		selected = project.Lock.Lock
	case Remove:
		if err := removePlugin(ctx, project, name, r.Options.Registry); err != nil {
			return 0, err
		}
		selected = project.Lock.Lock
	case Sync:
		selected, err = project.SelectedPlugins(ctx)
		if err != nil {
			return 0, err
		}
		if err := selected.RefreshStatuses(ctx, project.StatusOptions); err != nil {
			return 0, err
		}
		if err := plugininstall.Sync(ctx, selected, acquisition); err != nil {
			return 0, err
		}
		if err := project.SyncDirect(ctx); err != nil {
			return 0, err
		}
	case List:
		selected, err = project.SelectedPlugins(ctx)
		if err != nil {
			return 0, err
		}
	case Search, Versions:
		return 0, r.discover(ctx, streams, registry, name, acquisition)
	default:
		return 0, fmt.Errorf("unsupported plugin operation %q", r.Operation)
	}
	if r.JSON {
		return 0, commandstreams.WriteJSON(streams.Stdout, struct {
			plugininstall.Lock
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

func RequestedPlugin(name string, options RegistryOptions, existing []pluginref.Reference) (pluginref.Reference, error) {
	ref, err := pluginref.Parse(name)
	if err != nil {
		return pluginref.Reference{}, commandargs.UsageError{Cause: err}
	}
	if ref.DirectSource() != "" {
		if options.Version != "" || options.Registry != "" {
			return pluginref.Reference{}, commandargs.UsageError{Cause: errors.New("direct plugin URLs cannot use --version or --registry; change the URL and SHA256 pin instead")}
		}
		return ref, nil
	}
	if strings.Contains(name, "@") && options.Version != "" && ref.Version() != options.Version {
		return pluginref.Reference{}, commandargs.UsageError{Cause: errors.New("plugin @version conflicts with --version")}
	}
	index, err := RequestedPluginIndex(ref, options.Registry, existing)
	if err != nil {
		return pluginref.Reference{}, err
	}
	if index >= 0 && !strings.Contains(name, "@") && options.Version == "" && options.Registry == "" {
		return existing[index], nil
	}
	version, registry := ref.Version(), ref.Registry()
	if index >= 0 {
		registry = existing[index].Registry()
		if !strings.Contains(name, "@") {
			version = existing[index].Version()
		}
	}
	if options.Version != "" {
		version = options.Version
	}
	if options.Registry != "" {
		registry = options.Registry
	}
	ref, err = pluginref.New(ref.Name(), version, registry)
	if err != nil {
		return pluginref.Reference{}, commandargs.UsageError{Cause: err}
	}
	return ref, nil
}

// RequestedPluginIndex finds the existing declaration for this repository.
// Aliases stay scoped to the registry that assigns them.
func RequestedPluginIndex(ref pluginref.Reference, registry string, existing []pluginref.Reference) (int, error) {
	index, named, count := -1, -1, 0
	for i, old := range existing {
		if old.Name() != ref.Name() {
			continue
		}
		named, count = i, count+1
		if registry != "" && !plugininstall.SameRegistry(old.Registry(), registry) {
			continue
		}
		if index >= 0 {
			return -1, commandargs.UsageError{Cause: fmt.Errorf("plugin %q is declared in multiple registries; select --registry", ref.Name())}
		}
		index = i
	}
	if index < 0 && registry != "" && !ref.IsAlias() {
		if count > 1 {
			return -1, commandargs.UsageError{Cause: fmt.Errorf("plugin %q is declared in multiple registries; select an existing --registry", ref.Name())}
		}
		index = named
	}
	return index, nil
}

func (r RegistryRequest) install(ctx context.Context, project *plugininstall.Project, name string) error {
	refs, err := project.Dependencies(name == "")
	if err != nil {
		return err
	}
	if name != "" {
		// The declaration records the digest an oci:// tag names now.
		name, err = plugininstall.PinSource(ctx, name, project.StatusOptions.Acquisition)
		if err != nil {
			return err
		}
		ref, err := RequestedPlugin(name, r.Options, refs)
		if err != nil {
			return err
		}
		if ref.IsAlias() {
			index, lookupErr := DeclaredPlugin(ctx, project, refs, name, r.Options.Registry)
			if lookupErr == nil {
				explicitVersion := strings.Contains(name, "@") || r.Options.Version != ""
				name = refs[index].Name()
				if explicitVersion {
					name += "@" + ref.Version()
				}
				ref, err = RequestedPlugin(name, r.Options, refs)
				if err != nil {
					return err
				}
			} else {
				if _, ok := errors.AsType[undeclaredPluginError](lookupErr); !ok {
					return lookupErr
				}
			}
		}
		index, err := RequestedPluginIndex(ref, r.Options.Registry, refs)
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

func (r RegistryRequest) update(ctx context.Context, project *plugininstall.Project, name string) error {
	refs, err := project.Dependencies(false)
	if err != nil {
		return err
	}
	update := refs
	if name != "" {
		index, err := DeclaredPlugin(ctx, project, refs, name, r.Options.Registry)
		if err != nil {
			return err
		}
		requested, err := RequestedPlugin(name, r.Options, nil)
		if err != nil {
			return err
		}
		ref := refs[index]
		// A direct URL pins its artifact and has no version to replace.
		if ref.DirectSource() == "" && (r.Options.Version != "" || strings.Contains(name, "@")) {
			ref, err = pluginref.New(ref.Name(), requested.Version(), ref.Registry())
			if err != nil {
				return commandargs.UsageError{Cause: err}
			}
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

func removePlugin(ctx context.Context, project *plugininstall.Project, name, registry string) error {
	refs, err := project.Dependencies(false)
	if err != nil {
		return err
	}
	index, err := DeclaredPlugin(ctx, project, refs, name, registry)
	if err != nil {
		return err
	}
	refs = slices.Delete(refs, index, index+1)
	project.Manifest.References = slices.Clone(refs)
	selected, err := project.SelectedPlugins(ctx)
	if err != nil {
		return err
	}
	project.Lock.Lock = selected
	return project.Save(ctx, refs)
}

// DeclaredPlugin resolves command spelling against declarations and recorded
// pins only. A shorthand from another registry never selects this declaration.
func DeclaredPlugin(ctx context.Context, project *plugininstall.Project, refs []pluginref.Reference, name, registry string) (int, error) {
	requested, err := pluginref.Parse(name)
	if err != nil {
		return -1, commandargs.UsageError{Cause: err}
	}
	index := -1
	for i, ref := range refs {
		if registry != "" && !plugininstall.SameRegistry(ref.Registry(), registry) {
			continue
		}
		matches := requested.Name() == ref.Name()
		for _, pin := range project.Lock.Lock.Plugins {
			if pin.Matches(ref) && (strings.EqualFold(pin.Name, requested.Name()) || pin.Alias != "" && pin.Alias == requested.Name()) {
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
	if index < 0 && requested.IsAlias() {
		// Alias convenience uses the selected release's cached registry evidence,
		// scoped to the declaration's authority; config itself stays canonical.
		for i, ref := range refs {
			if ref.DirectSource() != "" || registry != "" && !plugininstall.SameRegistry(ref.Registry(), registry) {
				continue
			}
			client, err := plugininstall.NewWithGrant(ref.Registry(), plugininstall.StatusOptions{Network: project.StatusOptions.Network, Acquisition: project.StatusOptions.Acquisition})
			if err != nil {
				return -1, err
			}
			pin, err := client.Lookup(ctx, ref.Name(), ref.Version())
			if err != nil {
				return -1, err
			}
			if pin.Alias != requested.Name() {
				continue
			}
			if index >= 0 {
				return -1, fmt.Errorf("plugin %q is declared in multiple registries; select --registry", name)
			}
			index = i
		}
	}
	if index < 0 {
		return -1, undeclaredPluginError{name: name}
	}
	return index, nil
}

func (r RegistryRequest) discover(ctx context.Context, streams commandstreams.Streams, registry, name string, acquisition plugininstall.Acquisition) error {
	client, err := plugininstall.New(registry, acquisition)
	if err != nil {
		return commandargs.UsageError{Cause: err}
	}
	if r.Operation == Search {
		plugins, err := client.Search(ctx, name)
		if err != nil {
			return err
		}
		if r.JSON {
			return commandstreams.WriteJSON(streams.Stdout, plugins)
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
		return commandargs.UsageError{Cause: err}
	}
	versions, err := client.Versions(ctx, ref.Name())
	if err != nil {
		return err
	}
	if r.JSON {
		return commandstreams.WriteJSON(streams.Stdout, versions)
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
