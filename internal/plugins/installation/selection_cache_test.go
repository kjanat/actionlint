package installation_test

import (
	"testing"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
)

func TestCachedPluginSelector(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", emptyComponent)
	client := registryClient(t, registry)
	first, err := client.ResolveCached(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	requests := len(registry.Requests())
	registry.Add(t, "v2.0.0", emptyComponent)
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
