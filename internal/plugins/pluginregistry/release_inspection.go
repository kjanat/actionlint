package pluginregistry

import (
	"context"

	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/process"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests/pluginreference"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
)

// InspectRelease resolves a registry reference and verifies its downloaded metadata.
func InspectRelease(ctx context.Context, ref pluginreference.Reference, acquisition pluginacquisition.Settings) (pluginartifact.ArtifactProvenance, error) {
	client, err := New(ref.Registry(), acquisition)
	if err != nil {
		return pluginartifact.ArtifactProvenance{}, err
	}
	pin, err := client.Resolve(ctx, ref.Name(), ref.Version())
	if err != nil {
		return pluginartifact.ArtifactProvenance{}, err
	}
	if pin.Origin == "candidate" {
		if err := pin.importCandidate(ctx, acquisition.ArtifactOptions()); err != nil {
			return pluginartifact.ArtifactProvenance{}, err
		}
	}
	info, err := process.InspectArtifact(ctx, pin.Source(), acquisition.ArtifactOptions())
	if err != nil {
		return pluginartifact.ArtifactProvenance{}, err
	}
	if err := pin.Verify(info); err != nil {
		return pluginartifact.ArtifactProvenance{}, err
	}
	return info, nil
}
