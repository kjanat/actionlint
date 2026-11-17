package pluginregistry

import (
	"context"

	"actionlint.kjanat.dev/v2/internal/model/pluginreference"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/process"
)

// InspectRelease resolves a registry reference and verifies its downloaded metadata.
func InspectRelease(ctx context.Context, ref pluginreference.Reference, acquisition pluginacquisition.Options) (artifacts.ArtifactProvenance, error) {
	client, err := New(ref.Registry(), acquisition)
	if err != nil {
		return artifacts.ArtifactProvenance{}, err
	}
	pin, err := client.Resolve(ctx, ref.Name(), ref.Version())
	if err != nil {
		return artifacts.ArtifactProvenance{}, err
	}
	if pin.Origin == "candidate" {
		if err := pin.importCandidate(ctx, acquisition.ArtifactOptions()); err != nil {
			return artifacts.ArtifactProvenance{}, err
		}
	}
	info, err := process.InspectArtifact(ctx, pin.Source(), acquisition.ArtifactOptions())
	if err != nil {
		return artifacts.ArtifactProvenance{}, err
	}
	if err := pin.Verify(info); err != nil {
		return artifacts.ArtifactProvenance{}, err
	}
	return info, nil
}
