package process

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/files"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts/oci"
)

// ReadProviderArtifact parses a distributed process-provider manifest. A standalone
// JSON file uses a PATH command and inline transforms; OCI supplies blob layers.
func ReadProviderArtifact(artifact *artifacts.Artifact) (toolmanifest.Manifest, error) {
	manifest, err := toolmanifest.ParsePluginManifest(artifact.Bytes)
	if err != nil {
		return toolmanifest.Manifest{}, fmt.Errorf("plugin %s: %w", artifact.Info.Source, err)
	}
	if manifest.Transport != toolmanifest.TransportProcess {
		return toolmanifest.Manifest{}, fmt.Errorf("plugin %s: tool provider %s uses the %s transport; an OCI tool provider manifest must use the process transport, and a WebAssembly plugin is published as a %s layer", artifact.Info.Source, manifest.Rule.Name, manifest.Transport, artifacts.MediaTypeWasm)
	}
	if !artifacts.IsOCI(artifact.Info.Source) {
		if len(manifest.Artifacts) != 0 {
			return toolmanifest.Manifest{}, errors.New("a standalone process provider cannot include executable artifacts; package its blob graph as OCI")
		}
		for _, transform := range manifest.Transforms() {
			if transform != nil && transform.Digest != "" {
				return toolmanifest.Manifest{}, errors.New("a standalone process provider requires inline transforms; package digest-addressed programs as OCI")
			}
		}
	}
	return manifest, nil
}

// ValidateProviderArtifact compiles a distributed provider's schemas and jq
// programs. OCI transforms are fetched by digest, but the process entry is
// never resolved, probed, or invoked.
func ValidateProviderArtifact(ctx context.Context, artifact *artifacts.Artifact, options artifacts.Options) error {
	manifest, err := ReadProviderArtifact(artifact)
	if err != nil {
		return err
	}
	var programs toolmanifest.Programs
	if artifacts.IsOCI(artifact.Info.Source) {
		programs, err = toolmanifest.LoadPrograms(manifest, func(digest string) ([]byte, error) {
			return oci.Layer(ctx, artifact.Info.Source, strings.TrimPrefix(digest, "sha256:"), options)
		})
		if err != nil {
			return fmt.Errorf("plugin %s: %w", artifact.Info.Source, err)
		}
	}
	if err := ValidateManifest(manifest, programs); err != nil {
		return fmt.Errorf("plugin %s: %w", artifact.Info.Source, err)
	}
	return nil
}

// ValidateLocalProvider compiles a local provider manifest's schemas and jq
// programs without resolving, probing or invoking its process entry.
func ValidateLocalProvider(ctx context.Context, path string) error {
	manifest, programs, err := toolmanifest.ReadLocal(files.New(ctx, nil, nil), path)
	if err != nil {
		return err
	}
	if err := ValidateManifest(manifest, programs); err != nil {
		return fmt.Errorf("plugin %s: %w", path, err)
	}
	return nil
}

// InspectArtifact resolves source and returns its provenance. A process
// provider manifest, local or distributed, is validated first; its process
// entry is never resolved, probed or invoked.
func InspectArtifact(ctx context.Context, source string, options artifacts.Options) (artifacts.ArtifactProvenance, error) {
	if toolmanifest.IsLocalManifest(source) {
		if err := ValidateLocalProvider(ctx, source); err != nil {
			return artifacts.ArtifactProvenance{}, err
		}
	}
	artifact, err := artifacts.Resolve(ctx, source, options)
	if err != nil {
		return artifacts.ArtifactProvenance{}, err
	}
	if artifact.Info.Kind == artifacts.ProviderManifest && !toolmanifest.IsLocalManifest(source) {
		if err := ValidateProviderArtifact(ctx, artifact, options); err != nil {
			return artifacts.ArtifactProvenance{}, err
		}
	}
	return artifact.Info, nil
}
