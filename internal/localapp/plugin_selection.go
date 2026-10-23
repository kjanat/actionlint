package localapp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifact"
	"actionlint.kjanat.dev/v2/internal/projectpath"
)

// PluginSelection is CLI policy, separate from the analysis configuration.
// Explicit sources replace automatic selection. Explicit locks/configs are
// invocation-wide; automatic locks belong only to their nearest repository.
type PluginSelection struct {
	Paths      []string
	LockFile   string
	ConfigFile string
	Disabled   bool
	Overlays   []config.Overlay
}

type PluginSources struct {
	Paths    []string
	LockFile string
	// Inputs retains observed locks independently of plugin activation.
	Inputs    []string
	root      string
	automatic bool
	lock      *pluginregistry.Lock
	configs   []*config.Config
	managed   map[string]string
	artifacts map[string]*pluginartifact.Artifact
	overlays  []config.Overlay
}

type pluginSelectionMode uint8

const (
	pluginAnalysis pluginSelectionMode = iota
	pluginRuleDiscovery
	pluginCachePreview
)

func OpenPluginLock(ctx context.Context, directory, explicit, config string) (*pluginregistry.LockFile, error) {
	if explicit != "" {
		return pluginregistry.OpenLock(ctx, explicit)
	}
	if config != "" {
		return pluginregistry.OpenLock(ctx, filepath.Join(filepath.Dir(config), pluginregistry.LockName))
	}
	root := projectpath.RepositoryRoot(directory)
	if root == "" {
		return nil, errors.New("no repository found; select a lock with --plugin-lock")
	}
	return pluginregistry.OpenProjectLock(ctx, root)
}

func (s PluginSelection) Resolve(ctx context.Context, directory string, input application.Input) (PluginSources, error) {
	selected, err := s.Select(ctx, directory, input)
	if err != nil {
		return PluginSources{}, err
	}
	if err := selected.Hydrate(ctx); err != nil {
		return PluginSources{}, err
	}
	return selected, nil
}

// ResolveForRules selects declared plugins without validating unrelated lint
// settings. Invalid declarations, inheritance and pins remain errors.
func (s PluginSelection) ResolveForRules(ctx context.Context, directory string) (PluginSources, error) {
	selected, err := s.selectSources(ctx, directory, application.Input{Kind: application.Repository}, pluginRuleDiscovery)
	if err != nil {
		return PluginSources{}, err
	}
	if err := selected.Hydrate(ctx); err != nil {
		return PluginSources{}, err
	}
	return selected, nil
}

// Select reads declarations and pins without downloading artifacts or running
// plugin code. Hosts can restore caches before Hydrate verifies their contents.
func (s PluginSelection) Select(ctx context.Context, directory string, input application.Input) (PluginSources, error) {
	return s.selectSources(ctx, directory, input, pluginAnalysis)
}

// SelectForCache previews migratable lock-only legacy projects without changing
// the selection rules used by analysis or editor commands.
func (s PluginSelection) SelectForCache(ctx context.Context, directory string, input application.Input) (PluginSources, error) {
	return s.selectSources(ctx, directory, input, pluginCachePreview)
}

func (s PluginSelection) selectSources(ctx context.Context, directory string, input application.Input, mode pluginSelectionMode) (PluginSources, error) {
	if len(s.Paths) > 0 {
		if s.LockFile != "" {
			return PluginSources{}, errors.New("--plugin and --plugin-lock cannot be combined")
		}
		return PluginSources{Paths: slices.Clone(s.Paths)}, nil
	}
	if s.Disabled && s.LockFile == "" {
		return PluginSources{}, nil
	}
	if s.LockFile != "" {
		f, err := OpenPluginLock(ctx, directory, s.LockFile, s.ConfigFile)
		if err != nil {
			return PluginSources{}, err
		}
		if !f.Exists() {
			return PluginSources{}, fmt.Errorf("plugin lock does not exist: %s", f.Path)
		}
		selected, err := sourcesFromPins(f.Path, f.Lock, "")
		if err == nil {
			selected.Inputs = []string{f.Path}
		}
		return selected, err
	}
	if s.ConfigFile != "" {
		project, err := openPluginSelectionProjectMode(ctx, directory, s.ConfigFile, mode)
		if err != nil {
			return PluginSources{}, err
		}
		if mode == pluginRuleDiscovery && !project.Manifest.Exists {
			return PluginSources{}, fmt.Errorf("plugin configuration does not exist: %s", project.Manifest.Path)
		}
		return s.sourcesFromProject(project, "", mode)
	}
	starts := []string{directory}
	if input.Kind == application.Files && len(input.Paths) > 0 {
		starts = nil
		for _, path := range input.Paths {
			if !filepath.IsAbs(path) {
				path = filepath.Join(directory, path)
			}
			starts = append(starts, filepath.Dir(path))
		}
	} else if input.Kind == application.Stdin {
		// Match workspace.Session.Content by discovering project plugins for named, existing stdin paths.
		if input.Name == "" || input.Name == "<stdin>" {
			return PluginSources{}, nil
		}
		path := input.Name
		if !filepath.IsAbs(path) {
			path = filepath.Join(directory, path)
		}
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return PluginSources{}, nil
		}
		starts = []string{filepath.Dir(path)}
	}
	roots := map[string]bool{}
	var selected PluginSources
	var selectedProject *PluginProject
	var inputs []string
	var configs []*config.Config
	for _, start := range starts {
		root := pluginProjectRoot(start)
		if roots[root] {
			continue
		}
		roots[root] = true
		if root == "" {
			continue
		}
		project, err := openPluginSelectionProjectMode(ctx, root, "", mode)
		if err != nil {
			return PluginSources{}, err
		}
		prepared, err := s.prepareProject(project)
		if err != nil {
			return PluginSources{}, err
		}
		configs = append(configs, prepared)
		pins, err := s.selectedLock(project, mode, root)
		if err != nil {
			return PluginSources{}, err
		}
		if project.Lock.Exists() {
			inputs = append(inputs, project.Lock.Path)
		}
		if len(pins.Plugins) != 0 || len(project.DirectSources()) != 0 {
			selected = PluginSources{LockFile: project.Lock.Path, root: root, automatic: true}
			selectedProject = project
		}
	}
	if selectedProject == nil {
		if len(configs) != 0 {
			return PluginSources{Inputs: inputs, root: pluginProjectRoot(directory), automatic: true, configs: configs, overlays: slices.Clone(s.Overlays)}, nil
		}
		resolved, err := config.ResolveBeforePlugins(nil, s.Overlays)
		if err != nil {
			return PluginSources{}, err
		}
		selected := PluginSources{Inputs: inputs, root: pluginProjectRoot(directory), automatic: true, configs: []*config.Config{resolved.Config}, overlays: slices.Clone(s.Overlays)}
		for _, ref := range resolved.Config.Plugins {
			source := ref.DirectSource()
			if source == "" {
				return PluginSources{}, errors.New("registry plugin declarations require a repository or explicit config file with a lockfile")
			}
			selected.Paths = append(selected.Paths, source)
		}
		return selected, nil
	}
	if len(roots) != 1 {
		return PluginSources{}, errors.New("inputs span repositories with plugin configuration; check them separately or select --plugin-lock explicitly")
	}
	return s.sourcesFromProject(selectedProject, selected.root, mode)
}

func (s PluginSelection) prepareProject(project *PluginProject) (*config.Config, error) {
	resolved, err := config.ResolveBeforePlugins(project.Manifest.node, s.Overlays)
	if err != nil {
		return nil, err
	}
	project.Manifest.References = resolved.Config.Plugins
	project.Manifest.Declared = resolved.Config.Plugins != nil
	if len(project.Manifest.References) != 0 {
		// Check the complete source graph only after overlays determine whether
		// this invocation activates plugins. Ordinary config reads allow links.
		for path := range project.Manifest.snapshots {
			if err := safePluginManifestPath(path); err != nil {
				return nil, err
			}
		}
	}
	return resolved.Config, nil
}

func (s PluginSelection) sourcesFromProject(project *PluginProject, root string, mode pluginSelectionMode) (PluginSources, error) {
	prepared, err := s.prepareProject(project)
	if err != nil {
		return PluginSources{}, err
	}
	lock, err := s.selectedLock(project, mode, root)
	if err != nil {
		return PluginSources{}, err
	}
	path := project.Lock.Path
	if len(lock.Plugins) == 0 {
		path = ""
	}
	selected, err := sourcesFromPins(path, lock, root)
	if err != nil {
		return PluginSources{}, err
	}
	if project.Lock.Exists() {
		selected.Inputs = []string{project.Lock.Path}
	}
	selected.configs = []*config.Config{prepared}
	selected.overlays = slices.Clone(s.Overlays)
	for _, ref := range project.Manifest.References {
		if source := ref.DirectSource(); source != "" {
			selected.Paths = append(selected.Paths, source)
		}
	}
	return selected, nil
}

func sourcesFromPins(path string, lock pluginregistry.Lock, root string) (PluginSources, error) {
	if err := lock.Validate(); err != nil {
		return PluginSources{}, err
	}
	selected := PluginSources{LockFile: path, root: root, automatic: root != "", lock: &lock, managed: map[string]string{}}
	for _, p := range lock.Plugins {
		selected.Paths = append(selected.Paths, p.Source())
		if p.Name == "github.com/kjanat/actionlint-plugin-shellcheck" {
			selected.managed[p.Source()] = "shellcheck"
		}
	}
	return selected, nil
}

// Hydrate verifies pinned cached artifacts and downloads missing bytes.
func (s *PluginSources) Hydrate(ctx context.Context) error {
	if s.lock == nil {
		return nil
	}
	if err := s.lock.CheckStatuses(ctx, pluginregistry.StatusOptions{}); err != nil {
		return err
	}
	artifacts := make(map[string]*pluginartifact.Artifact, len(s.lock.Plugins))
	for _, pin := range s.lock.Plugins {
		artifact, err := pluginartifact.Resolve(ctx, pin.Source(), artifactOptions())
		if err != nil {
			return fmt.Errorf("plugin %s@%s: %w", pin.Name, pin.Version, err)
		}
		if err := pin.Verify(artifact.Info); err != nil {
			return fmt.Errorf("plugin %s@%s: %w", pin.Name, pin.Version, err)
		}
		artifacts[pin.Source()] = artifact
	}
	s.artifacts = artifacts
	return nil
}

// ValidateScope prevents a session's automatic project plugins from being
// applied to documents opened from another (including nested) repository.
func (s *PluginSources) ValidateScope(paths []string) error {
	if !s.automatic {
		return nil
	}
	for _, path := range paths {
		root := pluginProjectRoot(filepath.Dir(path))
		if root == s.root {
			continue
		}
		if len(s.Paths) == 0 {
			if root == "" {
				continue
			}
			project, err := openPluginSelectionProject(context.Background(), root, "")
			if err != nil {
				return err
			}
			if _, err := (PluginSelection{Overlays: s.overlays}).prepareProject(project); err != nil {
				return err
			}
			pins, err := project.SelectedLock()
			if err != nil {
				return err
			}
			if len(pins.Plugins) == 0 && len(project.DirectSources()) == 0 {
				continue
			}
		}
		if root != s.root {
			return fmt.Errorf("document %s is outside the plugin lock's repository; use a separate language server or --plugin-lock", path)
		}
	}
	return nil
}

func pluginProjectRoot(path string) string {
	root := projectpath.RepositoryRoot(path)
	if root == "" {
		return ""
	}
	if physical, err := filepath.EvalSymlinks(root); err == nil {
		return physical
	}
	return root
}

func SyncPlugins(ctx context.Context, lock pluginregistry.Lock) error {
	return lock.Sync(ctx, artifactOptions())
}
