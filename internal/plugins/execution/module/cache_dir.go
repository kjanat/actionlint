package module

import (
	"os"
	"path/filepath"
)

// DefaultCacheDir creates the user cache directory for compiled modules, or returns empty on failure.
func DefaultCacheDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(base, "actionlint", "wazero")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	return dir
}
