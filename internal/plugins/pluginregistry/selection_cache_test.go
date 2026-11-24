package pluginregistry_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sha256hex"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/component"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
)

func TestCachedPluginSelector(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", analyzerComponent)
	client := registryClient(t, registry)
	first, err := client.ResolveCached(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	requests := len(registry.Requests())
	registry.Add(t, "v2.0.0", analyzerComponent)
	for _, offline := range []bool{false, true} {
		ctx, _ := network.WithSession(t.Context(), network.Options{Offline: offline})
		got, err := client.ResolveCached(ctx, fixtureregistry.FixturePluginName, "latest")
		if err != nil || got != first || len(registry.Requests()) != requests {
			t.Fatalf("offline=%t selected=%+v error=%v requests=%v", offline, got, err, registry.Requests())
		}
	}
	ctx, _ := network.WithSession(t.Context(), network.Options{WarmCache: true})
	updated, err := client.ResolveCached(ctx, fixtureregistry.FixturePluginName, "latest")
	if err != nil || updated.Version != "v2.0.0" {
		t.Fatalf("refreshed selection=%+v error=%v", updated, err)
	}
	ctx, _ = network.WithSession(t.Context(), network.Options{Offline: true})
	got, err := client.ResolveCached(ctx, fixtureregistry.FixturePluginName, "latest")
	if err != nil || got != updated {
		t.Fatalf("cached refresh=%+v error=%v", got, err)
	}
}

func TestCachedRegistryUsesClientAuthority(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", analyzerComponent)
	client := registryClient(t, registry)
	pin, err := client.ResolveCached(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	cache, err := pluginacquisition.RegistryCacheDir(fixtureAcquisition(registry))
	if err != nil {
		t.Fatal(err)
	}
	cache = filepath.Join(cache, sha256hex.Digest([]byte(fixtureregistry.FixtureRegistryURL)))
	for _, entry := range []struct{ directory, selector, key string }{
		{"selections", "latest", pin.Name + "\nlatest\n" + component.AcceptedInterfacesDigest()},
		{"releases", pin.Version, pin.Name + "\n" + pin.Version},
	} {
		cached := pin
		cached.Selector = entry.selector
		cached.Registry = "https://catalogue.example:443/"
		data, err := json.Marshal(cached)
		if err != nil {
			t.Fatal(err)
		}
		name := sha256hex.Digest([]byte(entry.key)) + ".json"
		if err := os.WriteFile(filepath.Join(cache, entry.directory, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, _ := network.WithSession(t.Context(), network.Options{Offline: true})
	requests := len(registry.Requests())
	if got, err := client.ResolveCached(ctx, pin.Name, "latest"); err != nil || got != pin {
		t.Fatalf("cached selection registry = %q, want %q; error = %v", got.Registry, pin.Registry, err)
	}
	if got, err := client.Lookup(ctx, pin.Name, pin.Version); err != nil || got.Registry != pin.Registry {
		t.Fatalf("cached release registry = %q, want %q; error = %v", got.Registry, pin.Registry, err)
	}
	if len(registry.Requests()) != requests {
		t.Fatal("cached records should work offline")
	}
}

func TestReselectDefaultUsesBuiltInRegistry(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	publisher := fixtureregistry.PluginPublisher{PluginID: "no-sudo", BaseURL: pluginregistry.DefaultURL, Name: fixtureregistry.FixturePluginName, RepositoryID: 10}
	registry.AddPlugin(t, publisher, "v1.0.0", analyzerComponent)
	registry.AddPlugin(t, publisher, "v2.0.0", analyzerComponent)
	registry.PublishPlugin(t, publisher, "v1.0.0", "v2.0.0")
	options := pluginregistry.StatusOptions{Acquisition: fixtureAcquisition(registry)}
	options.Acquisition.Connections.Resolver = fixtureregistry.Resolver("plugins.actionlint.kjanat.dev")
	var err error
	options.Network, err = artifacts.ExplicitSource(pluginregistry.DefaultURL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := pluginregistry.New(pluginregistry.DefaultURL, options.Acquisition)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.ResolveCached(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil || pin.Version != "v2.0.0" {
		t.Fatalf("selection=%+v error=%v", pin, err)
	}
	cache, err := pluginacquisition.RegistryCacheDir(fixtureAcquisition(registry))
	if err != nil {
		t.Fatal(err)
	}
	selections := filepath.Join(cache, sha256hex.Digest([]byte(pluginregistry.DefaultURL)), "selections")
	selection := filepath.Join(selections, sha256hex.Digest([]byte(pin.Name+"\nlatest\n"+component.AcceptedInterfacesDigest()))+".json")
	fallback := filepath.Join(selections, sha256hex.Digest([]byte(pin.Name+"\nlatest\n"+component.AdmissionIdentity()))+".json")
	stale, err := os.ReadFile(selection)
	if err != nil {
		t.Fatal(err)
	}
	reason := &component.AdmissionError{Err: errors.New("rejected bytes")}
	foreign := pin
	foreign.Registry = fixtureregistry.FixtureRegistryURL
	registry.ResetRequests()
	if _, err := pluginregistry.ReselectDefault(t.Context(), foreign, reason, options); err == nil {
		t.Fatal("accepted a pin from another registry")
	}
	if requests := registry.Requests(); len(requests) != 0 {
		t.Fatalf("requested registry data for a foreign pin: %v", requests)
	}
	if next, err := pluginregistry.ReselectDefault(t.Context(), pin, reason, options); err != nil || next.Version != "v1.0.0" {
		t.Fatalf("reselected=%+v error=%v", next, err)
	}
	if err := os.Remove(fallback); err != nil {
		t.Fatal(err)
	}
	for _, offline := range []bool{true, false} {
		if err := os.WriteFile(selection, stale, 0o600); err != nil {
			t.Fatal(err)
		}
		ctx, _ := network.WithSession(t.Context(), network.Options{Offline: offline})
		got, err := client.ResolveCached(ctx, fixtureregistry.FixturePluginName, "latest")
		if offline && (!network.OnlyOffline(err) || got.Version != "") || !offline && (err != nil || got.Version != "v1.0.0") {
			t.Fatalf("offline=%t: selection=%+v error=%v", offline, got, err)
		}
	}
}
