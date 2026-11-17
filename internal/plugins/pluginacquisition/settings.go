// Package pluginacquisition holds the settings that locate plugin caches and
// make the network connections used to acquire plugins.
package pluginacquisition

import (
	"os"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/integrations/acquisitionhttp"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts/oci"
)

// Options selects the user cache directory and network connections for
// acquiring plugin artifacts and registry records.
type Options struct {
	UserCacheDir string
	Connections  acquisitionhttp.Connections
}

func (s Options) userCacheDir() (string, error) {
	if s.UserCacheDir != "" {
		return s.UserCacheDir, nil
	}
	return os.UserCacheDir()
}

// ArtifactCacheDir returns the directory that stores verified plugin artifacts.
func (s Options) ArtifactCacheDir() (string, error) {
	base, err := s.userCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "actionlint", "artifacts"), nil
}

// RegistryCacheDir returns the directory that stores verified registry
// releases, selections and release statuses.
func (s Options) RegistryCacheDir() (string, error) {
	base, err := s.userCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "actionlint", "registry"), nil
}

// ArtifactOptions caches artifacts in the user cache directory and pulls
// oci:// sources.
func (s Options) ArtifactOptions() artifacts.Options {
	options := artifacts.Options{PullOCI: oci.Pull, Connections: s.Connections}
	if directory, err := s.ArtifactCacheDir(); err == nil {
		options.CacheDir = directory
	}
	return options
}

// CompiledModuleCacheDir creates the user cache directory for compiled modules, or returns empty on failure.
func (s Options) CompiledModuleCacheDir() string {
	base, err := s.userCacheDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(base, "actionlint", "wazero")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	return dir
}
