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
	if s.LockFile != "" || s.ConfigFile != "" {
		f, err := OpenPluginLock(ctx, directory, s.LockFile, s.ConfigFile)
		if err != nil {
			return PluginSources{}, err
		}
		if !f.Exists() {
			if s.LockFile != "" {
				return PluginSources{}, fmt.Errorf("plugin lock does not exist: %s", f.Path)
			}
			return PluginSources{}, nil
		}
		return sourcesFromLock(ctx, f, "")
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
	} else if input.Kind == application.Stdin && input.Name != "" && input.Name != "<stdin>" {
		path := input.Name
		if !filepath.IsAbs(path) {
			path = filepath.Join(directory, path)
		}
		starts = []string{filepath.Dir(path)}
	}
	roots := map[string]bool{}
	var selected PluginSources
	var selectedFile *pluginregistry.LockFile
	for _, start := range starts {
		root := pluginProjectRoot(start)
		if roots[root] {
			continue
		}
		roots[root] = true
		if root == "" {
			continue
		}
		f, err := pluginregistry.OpenProjectLock(ctx, root)
		if err != nil {
			return PluginSources{}, err
		}
		if f.Exists() {
			selected = PluginSources{LockFile: f.Path, root: root, automatic: true}
			selectedFile = f
		}
	}
	if selected.LockFile == "" {
		return PluginSources{root: pluginProjectRoot(directory), automatic: true}, nil
	}
	if len(roots) != 1 {
		return PluginSources{}, errors.New("inputs span repositories with a plugin lock; check them separately or select --plugin-lock explicitly")
	}
	return sourcesFromLock(ctx, selectedFile, selected.root)
}

func sourcesFromLock(ctx context.Context, f *pluginregistry.LockFile, root string) (PluginSources, error) {
	if err := SyncPlugins(ctx, f.Lock); err != nil {
		return PluginSources{}, err
	}
	selected := PluginSources{LockFile: f.Path, root: root, automatic: root != ""}
	for _, p := range f.Lock.Plugins {
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
		if s.LockFile == "" {
			if root == "" {
				continue
			}
			_, err := os.Lstat(filepath.Join(root, ".github", pluginregistry.LockName))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
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
