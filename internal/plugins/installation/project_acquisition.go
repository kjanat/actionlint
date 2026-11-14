package installation

import (
	"context"
	"errors"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/input/configuration/address"
	"actionlint.kjanat.dev/v2/internal/input/discovery"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts/oci"
)

func OpenSelectedLock(ctx context.Context, directory, explicit, config string) (*LockFile, error) {
	if explicit != "" {
		return OpenLock(ctx, explicit)
	}
	if config != "" {
		if address.Remote(config) {
			return &LockFile{Lock: Lock{SchemaVersion: LockSchemaVersion}}, nil
		}
		return OpenLock(ctx, filepath.Join(filepath.Dir(config), LockName))
	}
	root := discovery.RepositoryRoot(directory)
	if root == "" {
		return nil, errors.New("no repository found; select a lock with --plugin-lock")
	}
	return OpenProjectLock(ctx, root)
}

// Sync downloads every locked plugin and, for each oci:// source the
// lock lists, every layer this host reads, such as a provider's executable.
// Afterwards the plugins run offline from the artifact store.
func Sync(ctx context.Context, lock Lock, acquisition Acquisition) error {
	options := acquisition.ArtifactOptions()
	if err := lock.Sync(ctx, options, StatusOptions{Acquisition: acquisition}); err != nil {
		return err
	}
	for _, pin := range lock.Plugins {
		if pluginartifact.IsOCI(pin.Source()) {
			if err := oci.PullLayers(ctx, pin.Source(), options); err != nil {
				return err
			}
		}
	}
	for _, pin := range lock.Sources {
		if err := oci.PullLayers(ctx, pin.Source(), options); err != nil {
			return err
		}
	}
	return nil
}
