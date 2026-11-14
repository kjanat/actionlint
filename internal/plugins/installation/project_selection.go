package installation

import (
	"context"

	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	configplugins "actionlint.kjanat.dev/v2/internal/input/configuration/plugins"
)

// OpenForSelection reads declarations without granting permission to edit a symlinked configuration.
func OpenForSelection(ctx context.Context, rules config.RuleSet, acquisition Acquisition, directory, configPath string, readMode configplugins.ReadMode) (*Project, error) {
	configPath, err := projectConfigPath(directory, configPath)
	if err != nil {
		return nil, err
	}
	manifest, err := configplugins.Read(ctx, rules, configPath, func(ctx context.Context, path string) ([]byte, error) {
		return configplugins.ReadSource(ctx, path, acquisition.Connections)
	}, readMode)
	if err != nil {
		return nil, err
	}
	return &Project{Manifest: manifest, StatusOptions: StatusOptions{Acquisition: acquisition}}, nil
}
