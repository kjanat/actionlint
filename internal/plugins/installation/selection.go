package installation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/address"
	configplugins "actionlint.kjanat.dev/v2/internal/input/configuration/plugins"

	"actionlint.kjanat.dev/v2/internal/input/discovery"

	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"

	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifacts"
)

// Selection chooses plugins from explicit arguments or repository configuration.
// Explicit sources replace automatic selection. Explicit configs are
// invocation-wide; automatic declarations belong to their nearest repository.
type Selection struct {
	RuleSet config.RuleSet
	Paths   []string

	ConfigFile string
	Disabled   bool
	Overlays   []config.Overlay
	// DefaultTools lists bundled linters that use registry plugins when configuration omits plugins.
	DefaultTools []string
	DisableTools bool
	// WithTools copies DisabledTools and NamedTools.
	DisabledTools, NamedTools []string
	Acquisition               Acquisition
}

// WithTools returns a copy of selection that stores the tool ids
// disabled by --no-tool or the GitHub Action's disable-tools input, and the
// tool ids named by --tool, --no-tool or that input. A selected plugin whose
// name or alias is a disabled id is never downloaded. Any other plugin is
// skipped before it runs when its metadata names a disabled id as its engine
// or one of its rules. After plugins load, each named id must name a bundled
// tool, a loaded plugin or a skipped plugin.
func WithTools(selection Selection, disabled, named []string) Selection {
	selection.DisabledTools, selection.NamedTools = slices.Clone(disabled), slices.Clone(named)
	return selection
}

// Sources records selected plugin references, release data and acquisition authority.
type Sources struct {
	Paths []string

	RepositoryRoot   string
	RepositoryScoped bool
	Releases         *ResolvedPlugins
	Configurations   []*config.Config
	Artifacts        map[string]*pluginartifact.Artifact
	// NetworkGrants come only from explicit source arguments, never project files.
	NetworkGrants map[string]acquisitionhttp.Grant
	Overlays      []config.Overlay
	// DisabledTools and NamedTools carry the invocation selections to loading.
	DisabledTools, NamedTools []string
	// SkippedTools lists disabled tool ids that matched a selected plugin.
	SkippedTools []string
	DisableTools bool
	// Configured identifies configuration-selected providers that require process opt-in.
	Configured  bool
	Acquisition Acquisition
}

func (s Selection) Resolve(ctx context.Context, directory string, input inputmodel.Request) (Sources, error) {
	selected, err := s.Select(ctx, directory, input)
	if err != nil {
		return Sources{}, err
	}
	if err := selected.Hydrate(ctx); err != nil {
		return Sources{}, err
	}
	return selected, nil
}

// ResolveForRules selects declared plugins without validating unrelated lint
// settings.
func (s Selection) ResolveForRules(ctx context.Context, directory string) (Sources, error) {
	selected, err := s.selectSources(ctx, directory, inputmodel.Request{Kind: inputmodel.Repository}, configplugins.PluginDeclarationsOnly)
	if err != nil {
		return Sources{}, err
	}
	if err := selected.Hydrate(ctx); err != nil {
		return Sources{}, err
	}
	return selected, nil
}

// Select resolves exact registry metadata without downloading artifacts or
// running plugin code. Hosts can restore artifacts before Hydrate verifies them.
func (s Selection) Select(ctx context.Context, directory string, input inputmodel.Request) (Sources, error) {
	return s.selectSources(ctx, directory, input, configplugins.CompleteConfiguration)
}

// SelectForCache selects the same declared releases as analysis without fetching
// artifacts or implicitly migrating project files.
func (s Selection) SelectForCache(ctx context.Context, directory string, input inputmodel.Request) (Sources, error) {
	return s.Select(ctx, directory, input)
}

func (s Selection) selectSources(ctx context.Context, directory string, input inputmodel.Request, mode configplugins.ReadMode) (Sources, error) {
	if s.DisableTools {
		s.DefaultTools = nil
	}
	selected, err := s.selectDeclared(ctx, directory, input, mode)
	if err != nil {
		return Sources{}, err
	}
	if len(s.DefaultTools) != 0 && len(s.Paths) == 0 && len(selected.Configurations) == 0 {
		project := &Project{Manifest: &configplugins.File{}, StatusOptions: StatusOptions{Acquisition: s.Acquisition}}
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

func (s Selection) selectDeclared(ctx context.Context, directory string, input inputmodel.Request, mode configplugins.ReadMode) (Sources, error) {
	if len(s.Paths) > 0 {
		return SourcesFromArguments(s.Paths, s.Acquisition)
	}
	if s.Disabled {
		return Sources{}, nil
	}
	if s.ConfigFile != "" {
		project, err := OpenForSelection(ctx, s.RuleSet, s.Acquisition, directory, s.ConfigFile, mode)
		if err != nil {
			return Sources{}, err
		}
		if !project.Manifest.Exists {
			if mode == configplugins.PluginDeclarationsOnly {
				return Sources{}, fmt.Errorf("plugin configuration does not exist: %s", project.Manifest.Path)
			}
			return Sources{}, fmt.Errorf("could not read config file %q: %w", project.Manifest.Path, os.ErrNotExist)
		}
		return s.sourcesFromProject(ctx, project, "")
	}
	starts := []string{directory}
	switch {
	case input.Kind == inputmodel.Repository && input.Directory != "":
		start := input.Directory
		if !filepath.IsAbs(start) {
			start = filepath.Join(directory, start)
		}
		starts = []string{start}
	case input.Kind == inputmodel.Files && len(input.Paths) > 0:
		starts = nil
		for _, path := range input.Paths {
			if !filepath.IsAbs(path) {
				path = filepath.Join(directory, path)
			}
			starts = append(starts, pluginSelectionStart(path))
		}
	case input.Kind == inputmodel.Stdin:
		// Match workspace.Session.Content by discovering project plugins for named, existing stdin paths.
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
	var selected Sources
	var selectedProject *Project
	var configs []*config.Config
	for _, start := range starts {
		root := discovery.PhysicalRepositoryRoot(start)
		if roots[root] {
			continue
		}
		roots[root] = true
		if root == "" {
			continue
		}
		project, err := OpenForSelection(ctx, s.RuleSet, s.Acquisition, root, "", mode)
		if err != nil {
			return Sources{}, err
		}
		prepared, err := s.prepareProject(project)
		if err != nil {
			return Sources{}, err
		}
		configs = append(configs, prepared)
		pins, err := project.SelectedPlugins(ctx)
		if err != nil {
			return Sources{}, err
		}
		if len(pins.Plugins) != 0 || len(project.DirectSources()) != 0 {
			selected = Sources{RepositoryRoot: root, RepositoryScoped: true}
			selectedProject = project
		}
	}
	if selectedProject == nil {
		if len(configs) != 0 {
			return Sources{RepositoryRoot: discovery.PhysicalRepositoryRoot(directory), RepositoryScoped: true, Configurations: configs, Overlays: slices.Clone(s.Overlays)}, nil
		}
		resolved, err := s.RuleSet.ResolveBeforePlugins(nil, s.Overlays)
		if err != nil {
			return Sources{}, err
		}
		selected := Sources{RepositoryRoot: discovery.PhysicalRepositoryRoot(directory), RepositoryScoped: true, Configurations: []*config.Config{resolved.Config}, Overlays: slices.Clone(s.Overlays), Configured: true}
		for _, ref := range resolved.Config.Plugins {
			source := ref.DirectSource()
			if source == "" {
				return Sources{}, errors.New("registry plugin declarations require a repository or explicit config file")
			}
			selected.Paths = append(selected.Paths, source)
		}
		if resolved.Config.Plugins == nil && len(s.DefaultTools) != 0 {
			project := &Project{Manifest: &configplugins.File{}, StatusOptions: StatusOptions{Acquisition: s.Acquisition}}
			return s.sourcesFromProject(ctx, project, "")
		}
		return selected, nil
	}
	if len(roots) != 1 {
		return Sources{}, errors.New("inputs span repositories with plugin configuration; check them separately or select --config explicitly")
	}
	return s.sourcesFromProject(ctx, selectedProject, selected.RepositoryRoot)
}

// Directory arguments belong to their own repository, not their parent's.
func pluginSelectionStart(path string) string {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return path
	}
	return filepath.Dir(path)
}

func (s Selection) prepareProject(project *Project) (*config.Config, error) {
	resolved, err := s.RuleSet.ResolveBeforePlugins(project.Manifest.MergedDocument(), s.Overlays)
	if err != nil {
		return nil, err
	}
	project.Manifest.References = resolved.Config.Plugins
	project.Manifest.Declared = resolved.Config.Plugins != nil
	if !project.Manifest.Declared && len(s.DefaultTools) != 0 {
		tools := slices.DeleteFunc(slices.Clone(s.DefaultTools), func(id string) bool { return slices.Contains(s.DisabledTools, id) })
		refs, err := DefaultReferences(resolved.Config, DefaultPluginOptions{Tools: tools, RuleSet: s.RuleSet})
		if err != nil {
			return nil, err
		}
		project.Manifest.References = refs
		project.Manifest.Declared = true
		project.defaultPlugins = true
	}
	if len(project.Manifest.References) != 0 && !project.defaultPlugins {
		// Check the complete source graph only after overlays determine whether
		// this invocation activates plugins. Ordinary config reads allow links.
		for path := range project.Manifest.SourceContents() {
			if address.Remote(path) {
				continue
			}
			if err := configplugins.ValidateLocalPath(path); err != nil {
				return nil, err
			}
		}
	}
	return resolved.Config, nil
}

func (s Selection) sourcesFromProject(ctx context.Context, project *Project, root string) (Sources, error) {
	prepared, err := s.prepareProject(project)
	if err != nil {
		return Sources{}, err
	}
	releases, err := project.SelectedPlugins(ctx)
	if err != nil {
		return Sources{}, err
	}
	selected, err := sourcesFromPins(releases, root)
	if err != nil {
		return Sources{}, err
	}
	selected.Configurations = []*config.Config{prepared}
	selected.Overlays = slices.Clone(s.Overlays)
	selected.Configured = true
	for _, ref := range project.Manifest.References {
		if source := ref.DirectSource(); source != "" {
			selected.Paths = append(selected.Paths, source)
		}
	}
	return selected, nil
}

func sourcesFromPins(releases ResolvedPlugins, root string) (Sources, error) {
	if err := releases.Validate(); err != nil {
		return Sources{}, err
	}
	selected := Sources{RepositoryRoot: root, RepositoryScoped: root != "", Releases: &releases}
	for _, p := range releases.Plugins {
		selected.Paths = append(selected.Paths, p.Source())
	}
	return selected, nil
}

// dropDisabledPins removes selected plugins whose name or alias is a disabled
// tool id, before Hydrate or loading could fetch them.
func (s *Sources) dropDisabledPins() {
	if s.Releases == nil || len(s.DisabledTools) == 0 && !s.DisableTools {
		return
	}
	releases := *s.Releases
	releases.Plugins = []Pin{}
	for _, pin := range s.Releases.Plugins {
		id := disabledPinTool(pin, s.DisabledTools)
		if s.DisableTools && (pin.Kind == pluginartifact.Core || pin.Kind == pluginartifact.ProviderManifest) {
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

// disabledPinTool returns the disabled tool id a pin's name or alias
// equals, or "".
func disabledPinTool(pin Pin, disabled []string) string {
	for _, name := range []string{pin.Alias, pin.Name} {
		if name != "" && slices.Contains(disabled, name) {
			return name
		}
	}
	return ""
}

// Hydrate verifies pinned cached artifacts and downloads missing bytes.
func (s *Sources) Hydrate(ctx context.Context) error {
	if s.Releases == nil {
		return nil
	}
	if err := s.Releases.CheckStatuses(ctx, StatusOptions{Acquisition: s.Acquisition}); err != nil {
		return err
	}
	artifacts := make(map[string]*pluginartifact.Artifact, len(s.Releases.Plugins))
	for _, pin := range s.Releases.Plugins {
		artifact, err := pluginartifact.Resolve(ctx, pin.Source(), s.ArtifactOptions(pin.Source()))
		if err != nil {
			return fmt.Errorf("plugin %s@%s: %w", pin.Name, pin.Version, err)
		}
		if err := pin.Verify(artifact.Info); err != nil {
			return fmt.Errorf("plugin %s@%s: %w", pin.Name, pin.Version, err)
		}
		artifacts[pin.Source()] = artifact
	}
	s.Artifacts = artifacts
	return nil
}

// ValidateScope prevents a session's automatic project plugins from being
// applied to documents opened from another (including nested) repository.
func (s *Sources) ValidateScope(ctx context.Context, rules config.RuleSet, paths []string) error {
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
			project, err := OpenForSelection(ctx, rules, s.Acquisition, root, "", configplugins.CompleteConfiguration)
			if err != nil {
				return err
			}
			if _, err := (Selection{RuleSet: rules, Overlays: s.Overlays}).prepareProject(project); err != nil {
				return err
			}
			pins, err := project.SelectedPlugins(ctx)
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
