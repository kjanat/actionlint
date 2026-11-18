package pluginregistry_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
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
		{"port leading zeros", "https://catalogue.example:0443/", "https://catalogue.example/", true},
		{"IDNA", "https://BÜCHER.example/", "https://xn--bcher-kva.example/", true},
		{"IPv6 spelling", "https://[0:0:0:0:0:0:0:1]/", "https://[::1]:443/", true},
		{"trailing dot", "https://catalogue.example./", "https://catalogue.example/", false},
		{"invalid endpoints", "http://catalogue.example/", "http://catalogue.example/", false},
		{"credentials", "https://user@catalogue.example/", "https://catalogue.example/", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := pluginregistry.SameRegistry(test.left, test.right); got != test.equal {
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
	file := pluginregistry.ResolvedPlugins{Plugins: []pluginregistry.Pin{legacy}}
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

func TestEquivalentRegistrySpellingsShareCache(t *testing.T) {
	t.Parallel()
	for _, spelling := range []string{"https://CATALOGUE.example:443/", "https://catalogue.example:0443/"} {
		t.Run(spelling, func(t *testing.T) {
			t.Parallel()
			registry := fixtureregistry.NewPluginRegistry(t)
			registry.Add(t, "v1.0.0", emptyComponent)
			pin, err := registryClient(t, registry).Resolve(t.Context(), fixtureregistry.FixturePluginName, "v1.0.0")
			if err != nil {
				t.Fatal(err)
			}
			publishStatuses(t, registry, releaseStatus(pin, pluginregistry.ReleaseRevoked))
			if _, err := registryClient(t, registry).RefreshStatuses(t.Context()); err != nil {
				t.Fatal(err)
			}
			equivalent, err := pluginregistry.New(spelling, fixtureAcquisition(registry))
			if err != nil {
				t.Fatal(err)
			}
			ctx, _ := network.WithSession(t.Context(), network.Options{Offline: true})
			requests := len(registry.Requests())
			cached, err := equivalent.Lookup(ctx, pin.Name, pin.Version)
			if err != nil {
				t.Fatalf("offline lookup through %s missed the release cached through %s: %v", spelling, fixtureregistry.FixtureRegistryURL, err)
			}
			if cached.Registry != fixtureregistry.FixtureRegistryURL {
				t.Fatalf("cached registry = %q, want %q", cached.Registry, fixtureregistry.FixtureRegistryURL)
			}
			respelled := pin
			respelled.Registry = spelling
			lock := pluginregistry.ResolvedPlugins{Plugins: []pluginregistry.Pin{respelled}}
			if err := lock.CheckStatuses(ctx, pluginregistry.StatusOptions{Acquisition: fixtureAcquisition(registry)}); err == nil || !strings.Contains(err.Error(), "revoked") {
				t.Fatalf("revocation recorded through %s did not block %s: %v", fixtureregistry.FixtureRegistryURL, spelling, err)
			}
			if len(registry.Requests()) != requests {
				t.Fatal("offline checks contacted the registry")
			}
		})
	}
}

func TestDuplicateAliasAcrossRegistrySpellings(t *testing.T) {
	t.Parallel()
	first := fixtureregistry.NewPluginRegistry(t)
	first.SetAliases("check")
	first.Add(t, "v1.0.0", emptyComponent)
	pin, err := registryClient(t, first).Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	second := fixtureregistry.NewPluginRegistry(t)
	second.AddPlugin(t, fixtureregistry.PluginPublisher{PluginID: "other-check", BaseURL: fixtureregistry.FixtureRegistryURL, Name: "github.com/publisher/other", Aliases: []string{"check"}, RepositoryID: 11}, "v1.0.0", emptyComponent)
	other, err := registryClient(t, second).Resolve(t.Context(), "github.com/publisher/other", "latest")
	if err != nil {
		t.Fatal(err)
	}
	if pin.Alias != "check" || other.Alias != "check" {
		t.Fatalf("fixture aliases = %q, %q", pin.Alias, other.Alias)
	}
	other.Registry = "https://catalogue.example:443/"
	plugins := pluginregistry.ResolvedPlugins{Plugins: []pluginregistry.Pin{pin, other}}
	if err := plugins.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate plugin alias") {
		t.Fatalf("Validate() = %v, want duplicate alias", err)
	}
}
