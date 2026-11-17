package dependencycache

import (
	"os"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/integrations/acquisitionhttp"
)

// Acquisition locates actionlint's dependency cache and selects the network
// connections that fill it. The zero value uses the operating system's user
// cache directory and the default connections.
type Acquisition struct {
	// UserCacheDir replaces the operating system's user cache directory.
	UserCacheDir string
	Connections  acquisitionhttp.Connections
}

// Directory returns actionlint's cache directory, the actionlint directory
// under the user cache directory. Every cached dependency, from plugin
// artifacts to action metadata and configuration files, is below it.
func (a Acquisition) Directory() (string, error) {
	base := a.UserCacheDir
	if base == "" {
		var err error
		if base, err = os.UserCacheDir(); err != nil {
			return "", err
		}
	}
	return filepath.Join(base, "actionlint"), nil
}
