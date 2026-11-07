package installation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/files"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts/oci"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/process"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

// ReadProviderArtifact parses a distributed process-provider manifest. A standalone
// JSON file uses a PATH command and inline transforms; OCI supplies blob layers.
func ReadProviderArtifact(artifact *pluginartifact.Artifact) (manifests.Manifest, error) {
	manifest, err := manifests.Parse(artifact.Bytes)
	if err != nil {
		return manifests.Manifest{}, fmt.Errorf("plugin %s: %w", artifact.Info.Source, err)
	}
	if manifest.Transport != manifests.TransportProcess {
		return manifests.Manifest{}, fmt.Errorf("plugin %s: tool provider %s uses the %s transport; an OCI tool provider manifest must use the process transport, and a WebAssembly plugin is published as a %s layer", artifact.Info.Source, manifest.Rule.Name, manifest.Transport, pluginartifact.MediaTypeWasm)
	}
	if !pluginartifact.IsOCI(artifact.Info.Source) {
		if len(manifest.Artifacts) != 0 {
			return manifests.Manifest{}, errors.New("a standalone process provider cannot include executable artifacts; package its blob graph as OCI")
		}
		for _, transform := range []*manifests.Transform{manifest.Process.Config.Transform, manifest.Process.Output.Transform} {
			if transform != nil && transform.Digest != "" {
				return manifests.Manifest{}, errors.New("a standalone process provider requires inline transforms; package digest-addressed programs as OCI")
			}
		}
	}
	return manifest, nil
}

// ValidateProviderArtifact compiles a distributed provider's schemas and jq
// programs. OCI transforms are fetched by digest, but the process entry is
// never resolved, probed, or invoked.
func ValidateProviderArtifact(ctx context.Context, artifact *pluginartifact.Artifact, options pluginartifact.Options) error {
	manifest, err := ReadProviderArtifact(artifact)
	if err != nil {
		return err
	}
	var programs manifests.Programs
	if pluginartifact.IsOCI(artifact.Info.Source) {
		programs, err = manifests.LoadPrograms(manifest, func(digest string) ([]byte, error) {
			return oci.Layer(ctx, artifact.Info.Source, strings.TrimPrefix(digest, "sha256:"), options)
		})
		if err != nil {
			return fmt.Errorf("plugin %s: %w", artifact.Info.Source, err)
		}
	}
	if err := process.ValidateManifest(manifest, programs); err != nil {
		return fmt.Errorf("plugin %s: %w", artifact.Info.Source, err)
	}
	return nil
}

func ValidateLocalProvider(ctx context.Context, path string) error {
	manifest, programs, err := manifests.ReadLocal(files.New(ctx, nil, nil), path)
	if err != nil {
		return err
	}
	if err := process.ValidateManifest(manifest, programs); err != nil {
		return fmt.Errorf("plugin %s: %w", path, err)
	}
	return nil
}
