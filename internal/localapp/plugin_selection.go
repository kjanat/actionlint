package localapp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/pluginregistry"
	"actionlint.kjanat.dev/internal/projectpath"
)

// PluginSelection is CLI policy, separate from the analysis configuration.
// Explicit sources replace automatic selection. Explicit locks/configs are
// invocation-wide; automatic locks belong only to their nearest repository.
type PluginSelection struct {
	Paths      []string
	LockFile   string
	ConfigFile string
	Disabled   bool
}

type PluginSources struct {
	Paths     []string
	LockFile  string
	root      string
	automatic bool
}

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
		return sourcesFromLock(ctx, f, "")
	}
	if s.ConfigFile != "" {
		project, err := openSelectedPluginProject(ctx, directory, s.ConfigFile)
		if err != nil {
			return PluginSources{}, err
		}
		return sourcesFromProject(ctx, project, "")
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
	for _, start := range starts {
		root := pluginProjectRoot(start)
		if roots[root] {
			continue
		}
		roots[root] = true
		if root == "" {
			continue
		}
		project, err := openSelectedPluginProject(ctx, root, "")
		if err != nil {
			return PluginSources{}, err
		}
		pins, err := project.SelectedLock()
		if err != nil {
			return PluginSources{}, err
		}
		if len(pins.Plugins) != 0 {
			selected = PluginSources{LockFile: project.Lock.Path, root: root, automatic: true}
			selectedProject = project
		}
	}
	if selected.LockFile == "" {
		return PluginSources{root: pluginProjectRoot(directory), automatic: true}, nil
	}
	if len(roots) != 1 {
		return PluginSources{}, errors.New("inputs span repositories with plugin configuration; check them separately or select --plugin-lock explicitly")
	}
	return sourcesFromProject(ctx, selectedProject, selected.root)
}

func sourcesFromLock(ctx context.Context, f *pluginregistry.LockFile, root string) (PluginSources, error) {
	return sourcesFromPins(ctx, f.Path, f.Lock, root)
}

func sourcesFromProject(ctx context.Context, project *PluginProject, root string) (PluginSources, error) {
	lock, err := project.SelectedLock()
	if err != nil {
		return PluginSources{}, err
	}
	path := project.Lock.Path
	if len(lock.Plugins) == 0 {
		path = ""
	}
	return sourcesFromPins(ctx, path, lock, root)
}

func sourcesFromPins(ctx context.Context, path string, lock pluginregistry.Lock, root string) (PluginSources, error) {
	if err := SyncPlugins(ctx, lock); err != nil {
		return PluginSources{}, err
	}
	selected := PluginSources{LockFile: path, root: root, automatic: root != ""}
	for _, p := range lock.Plugins {
		selected.Paths = append(selected.Paths, p.Source())
	}
	return selected, nil
}

// ValidateScope prevents a session's automatic project plugins from being
// applied to documents opened from another (including nested) repository.
func (s PluginSources) ValidateScope(paths []string) error {
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
			project, err := openSelectedPluginProject(context.Background(), root, "")
			if err != nil {
				return err
			}
			pins, err := project.SelectedLock()
			if err != nil {
				return err
			}
			if len(pins.Plugins) == 0 {
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
