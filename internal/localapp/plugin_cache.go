package localapp

import (
	"encoding/hex"
	"errors"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/config"
	toolconfig "actionlint.kjanat.dev/v2/internal/config/tools"
)

// PluginCachePlan describes portable artifact bytes, never compiled native code.
type PluginCachePlan struct {
	Directory string   `json:"directory"`
	Artifacts []string `json:"artifacts"`
}

// CachePlan does not fetch artifacts or execute plugins. Configuration backends
// are included so their pinned bytes can be restored before runtime loading.
func (s *PluginSources) CachePlan() (PluginCachePlan, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return PluginCachePlan{}, err
	}
	directory, err := filepath.Abs(filepath.Join(base, "actionlint", "artifacts"))
	if err != nil {
		return PluginCachePlan{}, err
	}
	plan := PluginCachePlan{Directory: directory, Artifacts: []string{}}
	paths := slices.Clone(s.Paths)
	add := func(backend *toolconfig.Backend) {
		if backend != nil && backend.Wasm != nil {
			paths = append(paths, backend.Wasm.Path)
		}
	}
	addTools := func(tools config.ToolsConfig) {
		for _, name := range slices.Sorted(maps.Keys(tools)) {
			add(tools[name].Backend)
		}
	}
	for _, cfg := range s.configs {
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
		location, err := url.Parse(path)
		if err != nil {
			return PluginCachePlan{}, err
		}
		if location.Scheme == "sha256" {
			decoded, err := hex.DecodeString(location.Host)
			if err != nil || len(decoded) != 32 || location.Host != strings.ToLower(location.Host) || location.User != nil || location.Path != "" || location.RawQuery != "" || location.ForceQuery || location.Fragment != "" {
				return PluginCachePlan{}, errors.New("cache URI must be sha256://<64 lowercase hex digits>")
			}
			plan.Artifacts = append(plan.Artifacts, location.Host)
			continue
		}
		if location.Scheme != "https" && location.Scheme != "http" || location.Fragment == "" {
			continue
		}
		digest, ok := strings.CutPrefix(location.Fragment, "sha256=")
		decoded, err := hex.DecodeString(digest)
		if !ok || err != nil || len(decoded) != 32 {
			return PluginCachePlan{}, errors.New("plugin URI fragment must be sha256=<64 hex digits>")
		}
		plan.Artifacts = append(plan.Artifacts, strings.ToLower(digest))
	}
	slices.Sort(plan.Artifacts)
	plan.Artifacts = slices.Compact(plan.Artifacts)
	return plan, nil
}
