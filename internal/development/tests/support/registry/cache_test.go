package registry

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestPluginRegistrySuppliesPrivateCacheAndLeavesProcessDefaults(t *testing.T) {
	t.Parallel()
	transport, resolver := http.DefaultTransport, net.DefaultResolver
	first, second := NewPluginRegistry(t), NewPluginRegistry(t)
	if http.DefaultTransport != transport || net.DefaultResolver != resolver {
		t.Fatal("registry fixture replaced the process HTTP transport or DNS resolver")
	}
	if first.Connections().Transport == nil || first.Connections().Resolver == nil {
		t.Fatalf("registry fixture connections = %+v", first.Connections())
	}
	if first.UserCacheDir() == second.UserCacheDir() {
		t.Fatalf("registries share cache directory %s", first.UserCacheDir())
	}
	if user, err := os.UserCacheDir(); err == nil && first.UserCacheDir() == user {
		t.Fatalf("registry fixture cache is the user cache %s", user)
	}
	entries, err := os.ReadDir(first.UserCacheDir())
	if err != nil || len(entries) != 0 {
		t.Fatalf("registry fixture cache is not fresh: %v %v", entries, err)
	}
	if err := os.MkdirAll(filepath.Join(first.UserCacheDir(), "actionlint", "artifacts"), 0o700); err != nil {
		t.Fatalf("registry fixture cache is not writable: %v", err)
	}
}
