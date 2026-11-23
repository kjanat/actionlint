package installation

import (
	"context"

	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
	"actionlint.kjanat.dev/v2/internal/input/configuration/plugins"
	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
)

// OpenForSelection grants no permission to edit a symlinked configuration, and files may be nil.
func OpenForSelection(ctx context.Context, rules configuration.RuleSet, acquisition dependencycache.Acquisition, directory, configPath string, readMode plugins.ReadMode, files *document.Files) (*Project, error) {
	configPath, err := projectConfigPath(directory, configPath)
	if err != nil {
		return nil, err
	}
	manifest, err := plugins.Read(ctx, rules, configPath, func(ctx context.Context, path string) ([]byte, error) {
		return plugins.ReadSource(ctx, path, acquisition.Connections)
	}, readMode, files)
	if err != nil {
		return nil, err
	}
	return &Project{Manifest: manifest, StatusOptions: pluginregistry.StatusOptions{Acquisition: acquisition}}, nil
}
