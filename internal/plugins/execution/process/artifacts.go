package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts/oci"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

// ProviderFiles describes a process plugin's manifest, transforms and executable path.
// Source is the OCI source when files are distributed as image layers.
type ProviderFiles struct {
	Manifest   manifests.Manifest
	Programs   manifests.Programs
	Executable string
	Source     string
}

// Materialize loads transform programs and prepares the executable selected for this host.
func (p ProviderFiles) Materialize(ctx context.Context, options pluginartifact.Options) (ProviderFiles, error) {
	layer := func(digest string) ([]byte, error) {
		return oci.Layer(ctx, p.Source, strings.TrimPrefix(digest, "sha256:"), options)
	}
	name := p.Manifest.Rule.Name
	programs, err := manifests.LoadPrograms(p.Manifest, layer)
	if err != nil {
		return ProviderFiles{}, fmt.Errorf("tool provider %s: %w", name, err)
	}
	p.Programs = programs
	if len(p.Manifest.Artifacts) == 0 {
		return p, nil
	}
	platform := pluginartifact.HostPlatform()
	index := slices.IndexFunc(p.Manifest.Artifacts, func(a manifests.Artifact) bool { return a.Platform == platform })
	if index < 0 {
		return ProviderFiles{}, fmt.Errorf("tool provider %s has no executable for %s", name, platform)
	}
	executable := p.Manifest.Artifacts[index]
	digest := strings.TrimPrefix(executable.Digest, "sha256:")
	store := pluginartifact.Store{Dir: options.CacheDir}
	p.Executable, err = store.Materialize(ctx, digest, executable.Filename, artifactMaxBytes)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, pluginartifact.ErrCorrupt) {
		// Fetch a missing or half-written executable, then copy it again.
		if _, err = layer(executable.Digest); err == nil {
			p.Executable, err = store.Materialize(ctx, digest, executable.Filename, artifactMaxBytes)
		}
	}
	if err != nil {
		return ProviderFiles{}, fmt.Errorf("tool provider %s: %w", name, err)
	}
	return p, nil
}

// artifactMaxBytes bounds a stored transform program or executable, as
// pluginartifact.Options.MaxBytes bounds a download by default.
const artifactMaxBytes = 128 << 20
