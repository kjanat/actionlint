package pluginregistry_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestSameRegistry(t *testing.T) {
	for _, test := range []struct {
		name, left, right string
		equal             bool
	}{
		{"host case", "https://CATALOGUE.example/plugins/", "https://catalogue.example/plugins/", true},
		{"canonical directory", "https://catalogue.example/plugins", "https://catalogue.example/plugins/", true},
		{"path case", "https://catalogue.example/Plugins/", "https://catalogue.example/plugins/", false},
		{"different host", "https://catalogue.example/plugins/", "https://other.example/plugins/", false},
		{"default port", "https://catalogue.example:443/", "https://catalogue.example/", true},
		{"IPv6 default port", "https://[::1]:443/", "https://[::1]/", true},
		{"IPv6 scope case", "https://[fe80::1%25Ethernet]/", "https://[fe80::1%25ethernet]/", false},
		{"different port", "https://catalogue.example:8443/", "https://catalogue.example/", false},
		{"invalid endpoints", "http://catalogue.example/", "http://catalogue.example/", false},
		{"credentials", "https://user@catalogue.example/", "https://catalogue.example/", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := pluginregistry.SameRegistry(test.left, test.right); got != test.equal {
				t.Fatalf("SameRegistry(%q, %q) = %v, want %v", test.left, test.right, got, test.equal)
			}
		})
	}
}

func TestLockSetAcceptsRegistryHostCase(t *testing.T) {
	registry := testutil.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", emptyComponent)
	pin, err := registryClient(t).Resolve(t.Context(), testutil.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	legacy := pin
	legacy.Registry = "https://CATALOGUE.example/"
	file, err := pluginregistry.OpenLock(t.Context(), filepath.Join(t.TempDir(), pluginregistry.LockName))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Set(legacy); err != nil {
		t.Fatal(err)
	}
	if err := file.Set(pin); err != nil {
		t.Fatalf("equivalent registry rejected: %v", err)
	}
	changed := pin
	changed.Registry = "https://other.example/"
	if err := file.Set(changed); err == nil {
		t.Fatal("different registry accepted")
	}
	changed = pin
	changed.Registry = legacy.Registry
	changed.SHA256 = strings.Repeat("0", 64)
	if err := file.Set(changed); err == nil {
		t.Fatal("host case bypassed immutable artifact validation")
	}
}

func TestLockCheckUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), pluginregistry.LockName)
	file, err := pluginregistry.OpenLock(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.CheckUnchanged(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"plugins":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := file.CheckUnchanged(t.Context()); err == nil {
		t.Fatal("concurrent creation accepted")
	}
}

func TestLockCheckUnchangedRejectsNewAncestorSymlink(t *testing.T) {
	directory := t.TempDir()
	parent := filepath.Join(directory, "config")
	target := filepath.Join(directory, "other")
	file, err := pluginregistry.OpenLock(t.Context(), filepath.Join(parent, pluginregistry.LockName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, parent); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := file.CheckUnchanged(t.Context()); err == nil {
		t.Fatal("new ancestor symlink accepted")
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("prevalidation wrote into redirected directory")
	}
}
