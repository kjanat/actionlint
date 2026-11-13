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
	RuleSet    config.RuleSet
	Paths      []string
	LockFile   string
	ConfigFile string
	Disabled   bool
	Overlays   []config.Overlay
	// WithTools copies DisabledTools and NamedTools.
	DisabledTools, NamedTools []string
	Acquisition               Acquisition
}

// WithTools returns a copy of selection that stores the tool ids
// disabled by --no-tool or the GitHub Action's disable-tools input, and the
// tool ids named by --tool, --no-tool or that input. A locked plugin whose
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
	Paths    []string
	LockFile string
	// Inputs lists observed lockfiles, including those for inactive plugins.
	Inputs           []string
	RepositoryRoot   string
	RepositoryScoped bool
	Releases         *Lock
	Configurations   []*config.Config
	Artifacts        map[string]*pluginartifact.Artifact
	// NetworkGrants come only from explicit source arguments, never project files.
	NetworkGrants map[string]acquisitionhttp.Grant
	Overlays      []config.Overlay
	// DisabledTools and NamedTools carry the invocation selections to loading.
	DisabledTools, NamedTools []string
	// SkippedTools lists disabled tool ids that matched a selected plugin.
	SkippedTools []string
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
	selected, err := s.selectDeclared(ctx, directory, input, mode)
	if err != nil {
		return Sources{}, err
	}
	selected.DisabledTools, selected.NamedTools = slices.Clone(s.DisabledTools), slices.Clone(s.NamedTools)
	selected.Acquisition = s.Acquisition
	selected.dropDisabledPins()
	return selected, nil
}

func (s Selection) selectDeclared(ctx context.Context, directory string, input inputmodel.Request, mode configplugins.ReadMode) (Sources, error) {
	if len(s.Paths) > 0 {
		if s.LockFile != "" {
			return Sources{}, errors.New("--plugin and --plugin-lock cannot be combined")
		}
		return SourcesFromArguments(s.Paths, s.Acquisition)
	}
	if s.Disabled && s.LockFile == "" {
		return Sources{}, nil
	}
	if s.LockFile != "" {
		f, err := OpenSelectedLock(ctx, directory, s.LockFile, s.ConfigFile)
		if err != nil {
			return Sources{}, err
		}
		if !f.Exists() {
			return Sources{}, fmt.Errorf("plugin lock does not exist: %s", f.Path)
		}
		selected, err := sourcesFromPins(f.Path, f.Lock, "")
		if err == nil {
			selected.Inputs = []string{f.Path}
		}
		return selected, err
	}
	if s.ConfigFile != "" {
		project, err := OpenForSelection(ctx, s.RuleSet, s.Acquisition, directory, s.ConfigFile, mode)
		if err != nil {
			return Sources{}, err
		}
		if mode == configplugins.PluginDeclarationsOnly && !project.Manifest.Exists {
			return Sources{}, fmt.Errorf("plugin configuration does not exist: %s", project.Manifest.Path)
		}
		return s.sourcesFromProject(ctx, project, "")
	}
	starts := []string{directory}
	if input.Kind == inputmodel.Files && len(input.Paths) > 0 {
		starts = nil
		for _, path := range input.Paths {
			if !filepath.IsAbs(path) {
				path = filepath.Join(directory, path)
			}
			starts = append(starts, pluginSelectionStart(path))
		}
	} else if input.Kind == inputmodel.Stdin {
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
	var inputs []string
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
		if project.Lock.Exists() {
			inputs = append(inputs, project.Lock.Path)
		}
		if len(pins.Plugins) != 0 || len(project.DirectSources()) != 0 {
			selected = Sources{RepositoryRoot: root, RepositoryScoped: true}
			selectedProject = project
		}
	}
	if selectedProject == nil {
		if len(configs) != 0 {
			return Sources{Inputs: inputs, RepositoryRoot: discovery.PhysicalRepositoryRoot(directory), RepositoryScoped: true, Configurations: configs, Overlays: slices.Clone(s.Overlays)}, nil
		}
		resolved, err := s.RuleSet.ResolveBeforePlugins(nil, s.Overlays)
		if err != nil {
			return Sources{}, err
		}
		selected := Sources{Inputs: inputs, RepositoryRoot: discovery.PhysicalRepositoryRoot(directory), RepositoryScoped: true, Configurations: []*config.Config{resolved.Config}, Overlays: slices.Clone(s.Overlays), Configured: true}
		for _, ref := range resolved.Config.Plugins {
			source := ref.DirectSource()
			if source == "" {
				return Sources{}, errors.New("registry plugin declarations require a repository or explicit config file")
			}
			selected.Paths = append(selected.Paths, source)
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
	if len(project.Manifest.References) != 0 {
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
	lock, err := project.SelectedPlugins(ctx)
	if err != nil {
		return Sources{}, err
	}
	selected, err := sourcesFromPins("", lock, root)
	if err != nil {
		return Sources{}, err
	}
	if project.Lock.Exists() {
		selected.Inputs = []string{project.Lock.Path}
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

func sourcesFromPins(path string, lock Lock, root string) (Sources, error) {
	if err := lock.Validate(); err != nil {
		return Sources{}, err
	}
	selected := Sources{LockFile: path, RepositoryRoot: root, RepositoryScoped: root != "", Releases: &lock}
	for _, p := range lock.Plugins {
		selected.Paths = append(selected.Paths, p.Source())
	}
	return selected, nil
}

// dropDisabledPins removes locked plugins whose name or alias is a disabled
// tool id, before Hydrate or loading could fetch them.
func (s *Sources) dropDisabledPins() {
	if s.Releases == nil || len(s.DisabledTools) == 0 {
		return
	}
	lock := *s.Releases
	lock.Plugins = []Pin{}
	for _, pin := range s.Releases.Plugins {
		id := disabledPinTool(pin, s.DisabledTools)
		if id == "" {
			lock.Plugins = append(lock.Plugins, pin)
			continue
		}
		s.SkippedTools = append(s.SkippedTools, id)
		s.Paths = slices.DeleteFunc(s.Paths, func(path string) bool { return path == pin.Source() })
	}
	s.Releases = &lock
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
	for _, pin := range s.Releases.Sources {
		if !slices.Contains(s.Paths, pin.Source()) {
			continue
		}
		artifact, err := pluginartifact.Resolve(ctx, pin.Source(), s.ArtifactOptions(pin.Source()))
		if err != nil {
			return err
		}
		if err := pin.Verify(artifact.Info); err != nil {
			return err
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
