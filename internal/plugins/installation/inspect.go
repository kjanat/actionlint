package installation

import (
	"context"
	"net/url"

	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts/oci"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/process"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
)

// PinSource resolves an OCI tag or an unpinned HTTPS artifact to its checksum.
// Installation saves that checksum so later checks use the same plugin bytes.
func PinSource(ctx context.Context, source string, acquisition pluginacquisition.Settings) (string, error) {
	if !pluginartifact.IsOCI(source) {
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
	grant, err := pluginartifact.ExplicitSource(source)
	if err != nil {
		return "", err
	}
	options := acquisition.ArtifactOptions()
	options.Network = grant
	return oci.Pin(ctx, source, options)
}

// Inspect resolves and reads metadata without creating a runtime.
func Inspect(ctx context.Context, source string, acquisition pluginacquisition.Settings) (pluginartifact.ArtifactProvenance, error) {
	grant, err := pluginartifact.ExplicitSource(source)
	if err != nil {
		return pluginartifact.ArtifactProvenance{}, err
	}
	options := acquisition.ArtifactOptions()
	options.Network = grant
	return process.InspectArtifact(ctx, source, options)
}
