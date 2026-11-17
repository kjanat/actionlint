package installation

import (
	"context"
	"net/url"

	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts/oci"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/process"
)

// PinSource resolves an OCI tag or an unpinned HTTPS artifact to its checksum.
// Installation saves that checksum so later checks use the same plugin bytes.
func PinSource(ctx context.Context, source string, acquisition pluginacquisition.Options) (string, error) {
	if !artifacts.IsOCI(source) {
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
	options := acquisition.ArtifactOptions()
	options.Network = grant
	return oci.Pin(ctx, source, options)
}

// Inspect resolves and reads metadata without creating a runtime.
func Inspect(ctx context.Context, source string, acquisition pluginacquisition.Options) (artifacts.ArtifactProvenance, error) {
	grant, err := artifacts.ExplicitSource(source)
	if err != nil {
		return artifacts.ArtifactProvenance{}, err
	}
	options := acquisition.ArtifactOptions()
	options.Network = grant
	return process.InspectArtifact(ctx, source, options)
}
