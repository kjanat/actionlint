package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPluginRegistryIsolatesUserCache(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "unwritable-cache-parent")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"XDG_CACHE_HOME", "LOCALAPPDATA", "HOME"} {
		t.Setenv(name, blocked)
	}
	t.Run("registry", func(t *testing.T) {
		NewPluginRegistry(t)
		cache, err := os.UserCacheDir()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(cache, "actionlint", "artifacts"), 0o700); err != nil {
			t.Fatalf("registry fixture inherited unwritable user cache: %v", err)
		}
	})
	for _, name := range []string{"XDG_CACHE_HOME", "LOCALAPPDATA", "HOME"} {
		if got := os.Getenv(name); got != blocked {
			t.Fatalf("fixture leaked %s=%q", name, got)
		}
	}
}
