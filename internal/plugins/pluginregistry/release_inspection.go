package pluginregistry

import (
	"context"

	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/model/pluginreference"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
	"actionlint.kjanat.dev/v2/internal/plugins/toolproviders"
)

// InspectRelease resolves a registry reference and verifies its downloaded metadata.
func InspectRelease(ctx context.Context, ref pluginreference.Reference, acquisition dependencycache.Acquisition) (artifacts.ArtifactProvenance, error) {
	client, err := New(ref.Registry(), acquisition)
	if err != nil {
		return artifacts.ArtifactProvenance{}, err
	}
	pin, err := client.Resolve(ctx, ref.Name(), ref.Version())
	if err != nil {
		return artifacts.ArtifactProvenance{}, err
	}
	if pin.Origin == "candidate" {
		if err := pin.importCandidate(ctx, pluginacquisition.ArtifactOptions(acquisition)); err != nil {
			return artifacts.ArtifactProvenance{}, err
		}
	}
	info, err := toolproviders.InspectArtifact(ctx, pin.Source(), pluginacquisition.ArtifactOptions(acquisition))
	if err != nil {
		return artifacts.ArtifactProvenance{}, err
	}
	if err := pin.Verify(info); err != nil {
		return artifacts.ArtifactProvenance{}, err
	}
	return info, nil
}
