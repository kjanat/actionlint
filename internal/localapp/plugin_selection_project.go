package localapp

import (
	"context"
	"path/filepath"
)

// openPluginSelectionProject preserves normal configuration read semantics.
// A symlinked config may be read; prepareProject checks source paths after
// overlays determine plugin activation. Installation/editing uses the stricter
// OpenPluginProject path.
func openPluginSelectionProject(ctx context.Context, directory, configPath string) (*PluginProject, error) {
	return openPluginSelectionProjectMode(ctx, directory, configPath, pluginAnalysis)
}

func openPluginSelectionProjectMode(ctx context.Context, directory, configPath string, mode pluginSelectionMode) (*PluginProject, error) {
	lock, err := OpenPluginLock(ctx, directory, "", configPath)
	if err != nil {
		return nil, err
	}
	if configPath == "" {
		configPath, err = pluginConfigPath(filepath.Dir(lock.Path))
		if err != nil {
			return nil, err
		}
	}
	manifest, err := openPluginManifestMode(ctx, configPath, readPluginManifestSource, mode)
	if err != nil {
		return nil, err
	}
	return &PluginProject{Manifest: manifest, Lock: lock}, nil
}
