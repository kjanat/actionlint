package installation

import (
	"os"
	"path/filepath"

	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts/oci"
)

// DefaultArtifactOptions caches artifacts in the user cache directory and pulls
// oci:// sources.
func DefaultArtifactOptions() pluginartifact.Options {
	base, err := os.UserCacheDir()
	if err != nil {
		return pluginartifact.Options{PullOCI: oci.Pull}
	}
	return pluginartifact.Options{CacheDir: filepath.Join(base, "actionlint", "artifacts"), PullOCI: oci.Pull}
}
