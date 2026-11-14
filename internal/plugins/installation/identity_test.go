package installation_test

import (
	"strings"
	"testing"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestSameRegistry(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			if got := plugininstall.SameRegistry(test.left, test.right); got != test.equal {
				t.Fatalf("SameRegistry(%q, %q) = %v, want %v", test.left, test.right, got, test.equal)
			}
		})
	}
}

func TestReleaseReplacementAcceptsRegistryHostCase(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", emptyComponent)
	pin, err := registryClient(t, registry).Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	legacy := pin
	legacy.Registry = "https://CATALOGUE.example/"
	file := plugininstall.ResolvedPlugins{Plugins: []plugininstall.Pin{legacy}}
	if err := file.CheckReplacement(pin, ""); err != nil {
		t.Fatal(err)
	}
	changed := pin
	changed.Registry = "https://other.example/"
	if err := file.CheckReplacement(changed, ""); err == nil {
		t.Fatal("different registry accepted")
	}
	changed = pin
	changed.Registry = legacy.Registry
	changed.SHA256 = strings.Repeat("0", 64)
	if err := file.CheckReplacement(changed, ""); err == nil {
		t.Fatal("host case bypassed immutable artifact validation")
	}
}
