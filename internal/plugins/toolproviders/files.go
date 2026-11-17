// Package toolproviders reads, validates and materializes tool provider
// plugins: process-transport manifests distributed as local files or OCI
// artifacts, together with their transform programs and executables. It
// fetches and checks the files; package externallinters/process runs them.
package toolproviders

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts/oci"
)

// ProviderFiles describes a process plugin's manifest, transforms and executable path.
// Source is the OCI source when files are distributed as image layers.
type ProviderFiles struct {
	Manifest   toolmanifest.Manifest
	Programs   toolmanifest.Programs
	Executable string
	Source     string
}

// Materialize loads transform programs and prepares the executable selected for this host.
func (p ProviderFiles) Materialize(ctx context.Context, options artifacts.Options) (ProviderFiles, error) {
	layer := func(digest string) ([]byte, error) {
		return oci.Layer(ctx, p.Source, strings.TrimPrefix(digest, "sha256:"), options)
	}
	name := p.Manifest.Rule.Name
	programs, err := toolmanifest.LoadPrograms(p.Manifest, layer)
	if err != nil {
		return ProviderFiles{}, fmt.Errorf("tool provider %s: %w", name, err)
	}
	p.Programs = programs
	if len(p.Manifest.Artifacts) == 0 {
		return p, nil
	}
	platform := artifacts.HostPlatform()
	index := slices.IndexFunc(p.Manifest.Artifacts, func(a toolmanifest.Artifact) bool { return a.Platform == platform })
	if index < 0 {
		return ProviderFiles{}, fmt.Errorf("tool provider %s has no executable for %s", name, platform)
	}
	executable := p.Manifest.Artifacts[index]
	digest := strings.TrimPrefix(executable.Digest, "sha256:")
	store := artifacts.Store{Dir: options.CacheDir}
	p.Executable, err = store.Materialize(ctx, digest, executable.Filename, artifactMaxBytes)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, artifacts.ErrCorrupt) {
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
