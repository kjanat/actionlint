package localapp

import (
	"errors"
	"os"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/pluginregistry"
)

func (s PluginSelection) selectedLock(project *PluginProject, mode pluginSelectionMode, root string) (pluginregistry.Lock, error) {
	if mode != pluginCachePreview || project.Manifest.Declared || len(project.Lock.Lock.Plugins) == 0 {
		return project.SelectedLock()
	}

	path := s.ConfigFile
	if path == "" {
		var err error
		path, err = pluginConfigPath(filepath.Join(root, ".github"))
		if err != nil {
			return pluginregistry.Lock{}, err
		}
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) || err == nil && !info.Mode().IsRegular() {
		return project.SelectedLock()
	}
	if err != nil {
		return pluginregistry.Lock{}, err
	}
	migration, err := config.PrepareMigration(path)
	if err != nil {
		return pluginregistry.Lock{}, err
	}
	if !migration.Changed {
		return project.SelectedLock()
	}
	if root != "" {
		inside, err := migrationPathInsideRepository(root, migration.Path)
		if err != nil {
			return pluginregistry.Lock{}, err
		}
		if !inside {
			return project.SelectedLock()
		}
	}

	refs, err := project.Dependencies(true)
	if err != nil {
		return pluginregistry.Lock{}, err
	}
	return project.selectedPins(refs)
}
