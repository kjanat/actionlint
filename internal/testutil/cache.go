package testutil

import "testing"

// IsolateUserCache gives each test a fresh cache on Linux, Windows, and macOS,
// including sandboxed builds whose inherited home directory is not writable.
func IsolateUserCache(t *testing.T) {
	t.Helper()
	cache := t.TempDir()
	for _, name := range []string{"XDG_CACHE_HOME", "LOCALAPPDATA", "HOME"} {
		t.Setenv(name, cache)
	}
}
