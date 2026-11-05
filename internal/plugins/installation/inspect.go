package installation

import (
	"context"

	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts/oci"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

// PinSource returns source with the tag of an
// oci://<host>/<repository>:<tag> source replaced by the digest the tag
// names now. Other sources are returned unchanged.
func PinSource(ctx context.Context, source string) (string, error) {
	if !pluginartifact.IsOCI(source) {
		return source, nil
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
