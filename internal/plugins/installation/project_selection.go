package installation

import (
	"context"

	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
	configplugins "actionlint.kjanat.dev/v2/internal/input/configuration/plugins"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
)

// OpenForSelection reads declarations without granting permission to edit a symlinked configuration.
// files shares the parsed sources with later loads in the same run; it may be nil.
func OpenForSelection(ctx context.Context, rules configuration.RuleSet, acquisition pluginacquisition.Settings, directory, configPath string, readMode configplugins.ReadMode, files *document.Files) (*Project, error) {
	configPath, err := projectConfigPath(directory, configPath)
	if err != nil {
		return nil, err
	}
	manifest, err := configplugins.Read(ctx, rules, configPath, func(ctx context.Context, path string) ([]byte, error) {
		return configplugins.ReadSource(ctx, path, acquisition.Connections)
	}, readMode, files)
	if err != nil {
		return nil, err
	}
	return &Project{Manifest: manifest, StatusOptions: pluginregistry.StatusOptions{Acquisition: acquisition}}, nil
}
