package pluginselection

import (
	"context"
	"errors"
	"maps"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/tools"
	"actionlint.kjanat.dev/v2/internal/model/pluginreference"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts/oci"
)

// CachePlan describes portable artifact bytes, never compiled native code.
type CachePlan struct {
	Directory string   `json:"directory"`
	Artifacts []string `json:"artifacts"`
}

// CachePlan may acquire OCI manifests to enumerate this host's verified blobs.
// It never fetches executable layers or executes plugins. Configuration backends
// are included so their pinned bytes can be restored before runtime loading.
func (s *Sources) CachePlan(ctx context.Context) (CachePlan, error) {
	cacheDir, err := s.Acquisition.ArtifactCacheDir()
	if err != nil {
		return CachePlan{}, err
	}
	directory, err := filepath.Abs(cacheDir)
	if err != nil {
		return CachePlan{}, err
	}
	plan := CachePlan{Directory: directory, Artifacts: []string{}}
	paths := slices.Clone(s.Paths)
	add := func(backend *tools.Backend) {
		if module, ok := backend.Wasm(); ok {
			paths = append(paths, module.Path())
		}
	}
	addTools := func(toolsConfig configuration.ToolsConfig) {
		for _, name := range slices.Sorted(maps.Keys(toolsConfig)) {
			add(toolsConfig[name].Backend)
		}
	}
	for _, cfg := range s.Configurations {
		addTools(cfg.Tools)
		for _, override := range cfg.Overrides {
			if override.Tools != nil {
				addTools(*override.Tools)
			}
		}
	}
	for _, path := range paths {
		if !strings.Contains(path, "://") {
			continue
		}
		if pluginreference.IsOCI(path) {
			digests, err := s.ociDigests(ctx, path)
			if err != nil {
				return CachePlan{}, err
			}
			plan.Artifacts = append(plan.Artifacts, digests...)
			continue
		}
		location, err := url.Parse(path)
		if err != nil {
			return CachePlan{}, err
		}
		if location.Scheme == "sha256" {
			if pluginreference.CheckDigest(location.Host) != nil || location.User != nil || location.Path != "" || location.RawQuery != "" || location.ForceQuery || location.Fragment != "" {
				return CachePlan{}, errors.New("cache URI must be sha256://<64 lowercase hex digits>")
			}
			plan.Artifacts = append(plan.Artifacts, location.Host)
			continue
		}
		if location.Scheme != "https" && location.Scheme != "http" || location.Fragment == "" {
			continue
		}
		digest, prefixed := strings.CutPrefix(location.Fragment, "sha256=")
		digest, ok := pluginreference.NormalizeDigest(digest)
		if !prefixed || !ok {
			return CachePlan{}, errors.New("plugin URI fragment must be sha256=<64 hex digits>")
		}
		plan.Artifacts = append(plan.Artifacts, digest)
	}
	slices.Sort(plan.Artifacts)
	plan.Artifacts = slices.Compact(plan.Artifacts)
	return plan, nil
}

// ociDigests derives required blobs from the configured digest and OCI manifests.
func (s *Sources) ociDigests(ctx context.Context, source string) ([]string, error) {
	if err := artifacts.RequireRemotePin(source); err != nil {
		return nil, err
	}
	return oci.Digests(ctx, source, s.ArtifactOptions(source))
}
