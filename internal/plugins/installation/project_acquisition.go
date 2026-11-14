package installation

import (
	"context"

	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts/oci"
)

// Sync downloads selected plugins and the OCI layers required by this host.
// Afterwards the plugins run offline from the artifact store.
func Sync(ctx context.Context, selected ResolvedPlugins, acquisition Acquisition) error {
	options := acquisition.ArtifactOptions()
	if err := selected.Sync(ctx, options, StatusOptions{Acquisition: acquisition}); err != nil {
		return err
	}
	for _, pin := range selected.Plugins {
		if pluginartifact.IsOCI(pin.Source()) {
			if err := oci.PullLayers(ctx, pin.Source(), options); err != nil {
				return err
			}
		}
	}
	return nil
}
