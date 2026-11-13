package installation

import (
	"context"
	"path/filepath"

	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	configplugins "actionlint.kjanat.dev/v2/internal/input/configuration/plugins"
)

// OpenForSelection reads declarations without granting permission to edit a symlinked configuration.
func OpenForSelection(ctx context.Context, rules config.RuleSet, acquisition Acquisition, directory, configPath string, readMode configplugins.ReadMode) (*Project, error) {
	lock, err := OpenSelectedLock(ctx, directory, "", configPath)
	if err != nil {
		return nil, err
	}
	if configPath == "" {
		configPath, err = configplugins.FindOrDefaultPath(filepath.Dir(lock.Path))
		if err != nil {
			return nil, err
		}
	}
	manifest, err := configplugins.Read(ctx, rules, configPath, func(ctx context.Context, path string) ([]byte, error) {
		return configplugins.ReadSource(ctx, path, acquisition.Connections)
	}, readMode)
	if err != nil {
		return nil, err
	}
	return &Project{Manifest: manifest, Lock: lock, StatusOptions: StatusOptions{Acquisition: acquisition}}, nil
}
