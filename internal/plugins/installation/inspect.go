package installation

import (
	"context"
	"net/url"

	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts/oci"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

// PinSource resolves an OCI tag or an unpinned HTTPS artifact to its checksum.
// Installation saves that checksum so later checks use the same plugin bytes.
func PinSource(ctx context.Context, source string) (string, error) {
	if !pluginartifact.IsOCI(source) {
		u, err := url.Parse(source)
		if err != nil || u.Scheme != "https" || u.Fragment != "" {
			return source, nil
		}
		info, err := Inspect(ctx, source)
		if err != nil {
			return "", err
		}
		u.Fragment = "sha256=" + info.SHA256
		return u.String(), nil
	}
	grant, err := pluginartifact.ExplicitSource(source)
	if err != nil {
		return "", err
	}
	options := DefaultArtifactOptions()
	options.Network = grant
	return oci.Pin(ctx, source, options)
}

// Inspect resolves and reads metadata without creating a runtime.
func Inspect(ctx context.Context, source string) (pluginartifact.Info, error) {
	if manifests.IsLocalManifest(source) {
		if err := ValidateLocalProvider(ctx, source); err != nil {
			return pluginartifact.Info{}, err
		}
	}
	grant, err := pluginartifact.ExplicitSource(source)
	if err != nil {
		return pluginartifact.Info{}, err
	}
	options := DefaultArtifactOptions()
	options.Network = grant
	artifact, err := pluginartifact.Resolve(ctx, source, options)
	if err != nil {
		return pluginartifact.Info{}, err
	}
	if artifact.Info.Kind == pluginartifact.ProviderManifest && !manifests.IsLocalManifest(source) {
		if err := ValidateProviderArtifact(ctx, artifact, options); err != nil {
			return pluginartifact.Info{}, err
		}
	}
	return artifact.Info, nil
}
