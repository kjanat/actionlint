// Package pluginacquisition holds the settings that locate plugin caches and
// make the network connections used to acquire plugins.
package pluginacquisition

import (
	"os"
	"path/filepath"

	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/acquisitionhttp"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts/oci"
)

// Settings selects the user cache directory and network connections for
// acquiring plugin artifacts and registry records.
type Settings struct {
	UserCacheDir string
	Connections  acquisitionhttp.Connections
}

func (s Settings) userCacheDir() (string, error) {
	if s.UserCacheDir != "" {
		return s.UserCacheDir, nil
	}
	return os.UserCacheDir()
}

// ArtifactCacheDir returns the directory that stores verified plugin artifacts.
func (s Settings) ArtifactCacheDir() (string, error) {
	base, err := s.userCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "actionlint", "artifacts"), nil
}

// RegistryCacheDir returns the directory that stores verified registry
// releases, selections and release statuses.
func (s Settings) RegistryCacheDir() (string, error) {
	base, err := s.userCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "actionlint", "registry"), nil
}

// ArtifactOptions caches artifacts in the user cache directory and pulls
// oci:// sources.
func (s Settings) ArtifactOptions() pluginartifact.Options {
	options := pluginartifact.Options{PullOCI: oci.Pull, Connections: s.Connections}
	if directory, err := s.ArtifactCacheDir(); err == nil {
		options.CacheDir = directory
	}
	return options
}

// CompiledModuleCacheDir creates the user cache directory for compiled modules, or returns empty on failure.
func (s Settings) CompiledModuleCacheDir() string {
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
