package installation

import (
	"os"
	"path/filepath"

	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts/oci"
)

type Acquisition struct {
	UserCacheDir string
	Connections  acquisitionhttp.Connections
}

func (a Acquisition) userCacheDir() (string, error) {
	if a.UserCacheDir != "" {
		return a.UserCacheDir, nil
	}
	return os.UserCacheDir()
}

// ArtifactOptions caches artifacts in the user cache directory and pulls
// oci:// sources.
func (a Acquisition) ArtifactOptions() pluginartifact.Options {
	options := pluginartifact.Options{PullOCI: oci.Pull, Connections: a.Connections}
	if base, err := a.userCacheDir(); err == nil {
		options.CacheDir = filepath.Join(base, "actionlint", "artifacts")
	}
	return options
}

// CompiledModuleCacheDir creates the user cache directory for compiled modules, or returns empty on failure.
func (a Acquisition) CompiledModuleCacheDir() string {
	base, err := a.userCacheDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(base, "actionlint", "wazero")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	return dir
}
