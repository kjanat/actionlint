// Package pluginselection chooses the plugins an invocation runs, from explicit
// source arguments or repository configuration, and verifies their artifacts.
package pluginselection

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"actionlint.kjanat.dev/v2/internal/input/checkrequest"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/configsource"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
	"actionlint.kjanat.dev/v2/internal/input/configuration/plugins"

	"actionlint.kjanat.dev/v2/internal/input/discovery"

	"actionlint.kjanat.dev/v2/internal/integrations/acquisitionhttp"
	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/integrations/network"

	"actionlint.kjanat.dev/v2/internal/model/pluginreference"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/installation"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/component"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
)

type PluginSelection struct {
	RuleSet configuration.RuleSet
	Paths   []string

	ConfigFile string
	// ConfigSources keeps the parsed configuration files of selection.
	ConfigSources             *document.Files
	Disabled                  bool
	Overlays                  []configuration.Overlay
	DefaultTools              []string
	lazyDefaults              bool
	DisableTools              bool
	DisabledTools, NamedTools []string
	Acquisition               dependencycache.Acquisition
}

func WithTools(selection PluginSelection, disabled, named []string) PluginSelection {
	selection.DisabledTools, selection.NamedTools = slices.Clone(disabled), slices.Clone(named)
	return selection
}

type Sources struct {
	Paths       []string
	LazySources map[string]pluginregistry.Pin

	RepositoryRoot   string
	RepositoryScoped bool
	Releases         *pluginregistry.ResolvedPlugins
	Configurations   []*configuration.Config
	Artifacts        map[string]*artifacts.Artifact
	// NetworkGrants come only from explicit source arguments and take nothing from project files.
	NetworkGrants             map[string]acquisitionhttp.Grant
	Overlays                  []configuration.Overlay
	DisabledTools, NamedTools []string
	SkippedTools              []string
	DisableTools              bool
	DefaultPlugins            bool
	Configured                bool
	Acquisition               dependencycache.Acquisition
}

func (s PluginSelection) Resolve(ctx context.Context, directory string, input checkrequest.Request) (Sources, error) {
	s.lazyDefaults = !network.Settings(ctx).WarmCache
	selected, err := s.Select(ctx, directory, input)
	if err != nil {
		return Sources{}, err
	}
	if err := selected.Hydrate(ctx); err != nil {
		return Sources{}, err
	}
	return selected, nil
}

func (s PluginSelection) ResolveForRules(ctx context.Context, directory string) (Sources, error) {
	selected, err := s.selectSources(ctx, directory, checkrequest.Request{Kind: checkrequest.Repository}, plugins.PluginDeclarationsOnly)
	if err != nil {
		return Sources{}, err
	}
	if err := selected.Hydrate(ctx); err != nil {
		return Sources{}, err
	}
	return selected, nil
}

// Select resolves exact registry metadata without downloading artifacts or
// running plugin code.
func (s PluginSelection) Select(ctx context.Context, directory string, input checkrequest.Request) (Sources, error) {
	return s.selectSources(ctx, directory, input, plugins.CompleteConfiguration)
}

func (s PluginSelection) SelectForCache(ctx context.Context, directory string, input checkrequest.Request) (Sources, error) {
	return s.Select(ctx, directory, input)
}

func (s PluginSelection) selectSources(ctx context.Context, directory string, input checkrequest.Request, mode plugins.ReadMode) (Sources, error) {
	if s.DisableTools {
		s.DefaultTools = nil
	}
	selected, err := s.selectDeclared(ctx, directory, input, mode)
	if err != nil {
		return Sources{}, err
	}
	if len(s.DefaultTools) != 0 && len(s.Paths) == 0 && len(selected.Configurations) == 0 {
		project := &installation.Project{Manifest: &plugins.File{}, StatusOptions: pluginregistry.StatusOptions{Acquisition: s.Acquisition}}
		selected, err = s.sourcesFromProject(ctx, project, "")
		if err != nil {
			return Sources{}, err
		}
	}
	selected.DisabledTools, selected.NamedTools = slices.Clone(s.DisabledTools), slices.Clone(s.NamedTools)
	selected.Acquisition = s.Acquisition
	selected.DisableTools = s.DisableTools
	selected.dropDisabledPins()
	return selected, nil
}

func (s PluginSelection) selectDeclared(ctx context.Context, directory string, input checkrequest.Request, mode plugins.ReadMode) (Sources, error) {
	if len(s.Paths) > 0 {
		return SourcesFromArguments(s.Paths, s.Acquisition)
	}
	if s.Disabled {
		return Sources{}, nil
	}
	if s.ConfigFile != "" {
		project, err := installation.OpenForSelection(ctx, s.RuleSet, s.Acquisition, directory, s.ConfigFile, mode, s.ConfigSources)
		if err != nil {
			return Sources{}, err
		}
		if !project.Manifest.Exists {
			if mode == plugins.PluginDeclarationsOnly {
				return Sources{}, fmt.Errorf("plugin configuration does not exist: %s", project.Manifest.Path)
			}
			return Sources{}, fmt.Errorf("could not read config file %q: %w", project.Manifest.Path, os.ErrNotExist)
		}
		return s.sourcesFromProject(ctx, project, "")
	}
	starts := []string{directory}
	switch {
	case input.Kind == checkrequest.Repository && input.Directory != "":
		start := input.Directory
		if !filepath.IsAbs(start) {
			start = filepath.Join(directory, start)
		}
		starts = []string{start}
	case input.Kind == checkrequest.Files && len(input.Paths) > 0:
		starts = nil
		for _, path := range input.Paths {
			if !filepath.IsAbs(path) {
				path = filepath.Join(directory, path)
			}
			starts = append(starts, RepositorySearchStart(path))
		}
	case input.Kind == checkrequest.Stdin:
		if input.Name == "" || input.Name == "<stdin>" {
			return Sources{}, nil
		}
		path := input.Name
		if !filepath.IsAbs(path) {
			path = filepath.Join(directory, path)
		}
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return Sources{}, nil
		}
		starts = []string{filepath.Dir(path)}
	}
	roots := map[string]bool{}
	var repositoryRoot string
	var skipped []string
	var selectedProject *installation.Project
	var configs []*configuration.Config
	for _, start := range starts {
		root := discovery.PhysicalRepositoryRoot(start)
		if roots[root] {
			continue
		}
		roots[root] = true
		if root == "" {
			continue
		}
		project, err := installation.OpenForSelection(ctx, s.RuleSet, s.Acquisition, root, "", mode, s.ConfigSources)
		if err != nil {
			return Sources{}, err
		}
		prepared, err := s.prepareProject(project)
		if err != nil {
			return Sources{}, err
		}
		configs = append(configs, prepared)
		pins, projectSkipped, err := s.selectedPlugins(ctx, project)
		if err != nil {
			return Sources{}, err
		}
		skipped = append(skipped, projectSkipped...)
		if len(pins.Plugins) != 0 || len(project.DirectSources()) != 0 {
			repositoryRoot = root
			selectedProject = project
		}
	}
	if selectedProject == nil {
		if len(configs) != 0 {
			return Sources{RepositoryRoot: discovery.PhysicalRepositoryRoot(directory), RepositoryScoped: true, Configurations: configs, Overlays: slices.Clone(s.Overlays), SkippedTools: skipped}, nil
		}
		resolved, err := s.RuleSet.ResolveBeforePlugins(nil, s.Overlays)
		if err != nil {
			return Sources{}, err
		}
		selected := Sources{RepositoryRoot: discovery.PhysicalRepositoryRoot(directory), RepositoryScoped: true, Configurations: []*configuration.Config{resolved.Config}, Overlays: slices.Clone(s.Overlays), Configured: true}
		for _, ref := range resolved.Config.Plugins {
			source := ref.DirectSource()
			if source == "" {
				return Sources{}, errors.New("registry plugin declarations require a repository or explicit config file")
			}
			selected.Paths = append(selected.Paths, source)
		}
		if resolved.Config.Plugins == nil && len(s.DefaultTools) != 0 {
			project := &installation.Project{Manifest: &plugins.File{}, StatusOptions: pluginregistry.StatusOptions{Acquisition: s.Acquisition}}
			return s.sourcesFromProject(ctx, project, "")
		}
		return selected, nil
	}
	if len(roots) != 1 {
		return Sources{}, errors.New("inputs span repositories with plugin configuration; check them separately or select --config explicitly")
	}
	return s.sourcesFromProject(ctx, selectedProject, repositoryRoot)
}

func RepositorySearchStart(path string) string {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return path
	}
	return filepath.Dir(path)
}

func (s PluginSelection) prepareProject(project *installation.Project) (*configuration.Config, error) {
	resolved, err := s.RuleSet.ResolveBeforePlugins(project.Manifest.MergedDocument(), s.Overlays)
	if err != nil {
		return nil, err
	}
	project.Manifest.References = resolved.Config.Plugins
	project.Manifest.Declared = resolved.Config.Plugins != nil
	if !project.Manifest.Declared && len(s.DefaultTools) != 0 {
		tools := slices.DeleteFunc(slices.Clone(s.DefaultTools), func(id string) bool { return slices.Contains(s.DisabledTools, id) })
		refs, err := installation.DefaultReferences(resolved.Config, installation.DefaultPluginOptions{Tools: tools, RuleSet: s.RuleSet})
		if err != nil {
			return nil, err
		}
		project.Manifest.References = refs
		project.Manifest.Declared = true
		project.DefaultPlugins = true
	}
	if len(project.Manifest.References) != 0 && !project.DefaultPlugins {
		for path := range project.Manifest.SourceContents() {
			if configsource.IsRemoteAddress(path) {
				continue
			}
			if err := plugins.ValidateLocalPath(path); err != nil {
				return nil, err
			}
		}
	}
	return resolved.Config, nil
}

func (s PluginSelection) sourcesFromProject(ctx context.Context, project *installation.Project, root string) (Sources, error) {
	prepared, err := s.prepareProject(project)
	if err != nil {
		return Sources{}, err
	}
	releases, skipped, err := s.selectedPlugins(ctx, project)
	if err != nil {
		return Sources{}, err
	}
	selected, err := sourcesFromPins(releases, root)
	if err != nil {
		return Sources{}, err
	}
	selected.SkippedTools = skipped
	selected.Configurations = []*configuration.Config{prepared}
	selected.Overlays = slices.Clone(s.Overlays)
	selected.Configured = true
	selected.DefaultPlugins = project.DefaultPlugins
	if s.lazyDefaults && project.DefaultPlugins {
		selected.LazySources = map[string]pluginregistry.Pin{}
		for _, pin := range releases.Plugins {
			if pin.Metadata != "" && (pin.Kind == artifacts.Core || pin.Kind == artifacts.Component && pin.Roles.ScriptOnly()) {
				selected.LazySources[pin.Source()] = pin
			}
		}
	}
	for _, ref := range project.Manifest.References {
		if source := ref.DirectSource(); source != "" {
			selected.Paths = append(selected.Paths, source)
		}
	}
	return selected, nil
}

func (s PluginSelection) selectedPlugins(ctx context.Context, project *installation.Project) (pluginregistry.ResolvedPlugins, []string, error) {
	selected := pluginregistry.ResolvedPlugins{Plugins: []pluginregistry.Pin{}}
	var skipped []string
	if s.DisableTools {
		options := network.Settings(ctx)
		options.Offline, options.WarmCache = true, false
		ctx, _ = network.WithSession(ctx, options)
	}
	for _, ref := range project.Manifest.References {
		if ref.DirectSource() != "" {
			continue
		}
		if slices.Contains(s.DisabledTools, ref.Name()) {
			skipped = append(skipped, ref.Name())
			continue
		}
		pins, err := project.SelectedPlugins(ctx, []pluginreference.Reference{ref})
		if err != nil {
			if s.DisableTools && network.OnlyOffline(err) {
				skipped = append(skipped, ref.Name())
				continue
			}
			if project.DefaultPlugins && errors.Is(err, pluginregistry.ErrNoCompatibleRelease) {
				network.Warn(ctx, "Skipping default plugin: "+err.Error())
				skipped = append(skipped, ref.Name())
				continue
			}
			return pluginregistry.ResolvedPlugins{}, nil, err
		}
		selected.Plugins = append(selected.Plugins, pins.Plugins...)
	}
	return selected, skipped, selected.Validate()
}

func sourcesFromPins(releases pluginregistry.ResolvedPlugins, root string) (Sources, error) {
	if err := releases.Validate(); err != nil {
		return Sources{}, err
	}
	selected := Sources{RepositoryRoot: root, RepositoryScoped: root != "", Releases: &releases}
	for _, p := range releases.Plugins {
		selected.Paths = append(selected.Paths, p.Source())
	}
	return selected, nil
}

func (s *Sources) dropDisabledPins() {
	if s.Releases == nil || len(s.DisabledTools) == 0 && !s.DisableTools {
		return
	}
	releases := *s.Releases
	releases.Plugins = []pluginregistry.Pin{}
	for _, pin := range s.Releases.Plugins {
		id := disabledPinTool(pin, s.DisabledTools)
		if s.DisableTools && (pin.Kind == artifacts.Core || pin.Kind == artifacts.ProviderManifest) {
			id = pin.Name
		}
		if id == "" {
			releases.Plugins = append(releases.Plugins, pin)
			continue
		}
		s.SkippedTools = append(s.SkippedTools, id)
		s.Paths = slices.DeleteFunc(s.Paths, func(path string) bool { return path == pin.Source() })
	}
	s.Releases = &releases
}

func disabledPinTool(pin pluginregistry.Pin, disabled []string) string {
	for _, name := range []string{pin.Alias, pin.Name} {
		if name != "" && slices.Contains(disabled, name) {
			return name
		}
	}
	return ""
}

func (s *Sources) Hydrate(ctx context.Context) error {
	if s.DisableTools {
		options := network.Settings(ctx)
		options.Offline, options.WarmCache = true, false
		ctx, _ = network.WithSession(ctx, options)
	}
	if s.Releases == nil {
		return nil
	}
	if err := s.Releases.CheckStatuses(ctx, pluginregistry.StatusOptions{Acquisition: s.Acquisition}); err != nil {
		return err
	}
	pluginArtifacts := make(map[string]*artifacts.Artifact, len(s.Releases.Plugins))
	for i := 0; i < len(s.Releases.Plugins); i++ {
		pin := s.Releases.Plugins[i]
		if _, lazy := s.LazySources[pin.Source()]; lazy {
			continue
		}
		artifact, err := artifacts.Resolve(ctx, pin.Source(), s.ArtifactOptions(pin.Source()))
		if err != nil {
			if s.DisableTools && network.OnlyOffline(err) {
				s.Paths = slices.DeleteFunc(s.Paths, func(path string) bool { return path == pin.Source() })
				continue
			}
			return fmt.Errorf("plugin %s@%s: %w", pin.Name, pin.Version, err)
		}
		if err := pin.Verify(artifact.Info); err != nil {
			return fmt.Errorf("plugin %s@%s: %w", pin.Name, pin.Version, err)
		}
		if s.DefaultPlugins && pin.Kind == artifacts.Component {
			if _, rejected := component.Admit(ctx, component.ComponentBytes(artifact.Bytes)); rejected != nil {
				if err := s.reselect(ctx, i, rejected); err != nil {
					return err
				}
				i--
				continue
			}
		}
		pluginArtifacts[pin.Source()] = artifact
	}
	s.Artifacts = pluginArtifacts
	return nil
}

func (s *Sources) reselect(ctx context.Context, index int, reason error) error {
	pin := s.Releases.Plugins[index]
	releases := *s.Releases
	releases.Plugins = slices.Clone(s.Releases.Plugins)
	next, err := pluginregistry.Reselect(ctx, pin, reason, s.Acquisition)
	switch {
	case errors.Is(err, pluginregistry.ErrNoCompatibleRelease):
		network.Warn(ctx, "Skipping default plugin: "+err.Error())
		releases.Plugins = slices.Delete(releases.Plugins, index, index+1)
		s.Paths = slices.DeleteFunc(s.Paths, func(path string) bool { return path == pin.Source() })
		name := pin.Alias
		if name == "" {
			name = pin.Name
		}
		s.SkippedTools = append(s.SkippedTools, name)
	case err != nil:
		return err
	default:
		releases.Plugins[index] = next
		s.Paths[slices.Index(s.Paths, pin.Source())] = next.Source()
	}
	s.Releases = &releases
	return nil
}

func (s *Sources) ValidateScope(ctx context.Context, rules configuration.RuleSet, paths []string) error {
	if !s.RepositoryScoped {
		return nil
	}
	for _, path := range paths {
		root := discovery.PhysicalRepositoryRoot(filepath.Dir(path))
		if root == s.RepositoryRoot {
			continue
		}
		if len(s.Paths) == 0 {
			if root == "" {
				continue
			}
			project, err := installation.OpenForSelection(ctx, rules, s.Acquisition, root, "", plugins.CompleteConfiguration, nil)
			if err != nil {
				return err
			}
			if _, err := (PluginSelection{RuleSet: rules, Overlays: s.Overlays}).prepareProject(project); err != nil {
				return err
			}
			pins, err := project.SelectedPlugins(ctx, project.Manifest.References)
			if err != nil {
				return err
			}
			if len(pins.Plugins) == 0 && len(project.DirectSources()) == 0 {
				continue
			}
		}
		if root != s.RepositoryRoot {
			return fmt.Errorf("document %s is outside the plugin configuration's repository; use a separate language server or --config", path)
		}
	}
	return nil
}
