package installation

import (
	"context"
	"net/url"

	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/model/pluginreference"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts/oci"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
	"actionlint.kjanat.dev/v2/internal/plugins/toolproviders"
)

func PinSource(ctx context.Context, source string, acquisition dependencycache.Acquisition) (string, error) {
	if !pluginreference.IsOCI(source) {
		u, err := url.Parse(source)
		if err == nil && u.Scheme == "https" && u.Fragment == "" {
			info, err := Inspect(ctx, source, acquisition)
			if err != nil {
				return "", err
			}
			u.Fragment = "sha256=" + info.SHA256
			return u.String(), nil
		}
		return source, nil
	}
	grant, err := artifacts.ExplicitSource(source)
	if err != nil {
		return "", err
	}
	options := pluginacquisition.ArtifactOptions(acquisition)
	options.Network = grant
	return oci.Pin(ctx, source, options)
}

// Inspect resolves and reads metadata without creating a runtime.
func Inspect(ctx context.Context, source string, acquisition dependencycache.Acquisition) (artifacts.ArtifactProvenance, error) {
	grant, err := artifacts.ExplicitSource(source)
	if err != nil {
		return artifacts.ArtifactProvenance{}, err
	}
	options := pluginacquisition.ArtifactOptions(acquisition)
	options.Network = grant
	return toolproviders.InspectArtifact(ctx, source, options)
}
