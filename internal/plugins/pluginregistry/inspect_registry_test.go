package pluginregistry_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
	"actionlint.kjanat.dev/v2/internal/model/pluginreference"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
)

func TestInspectCachedReleaseOffline(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", analyzerComponent)
	ref, err := pluginreference.New(fixtureregistry.FixturePluginName, "v1.0.0", fixtureregistry.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	acquisition := fixtureAcquisition(registry)
	want, err := pluginregistry.InspectRelease(t.Context(), ref, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	registry.SetOffline(true)
	registry.ResetRequests()
	ctx, _ := network.WithSession(t.Context(), network.Options{Offline: true})
	got, err := pluginregistry.InspectRelease(ctx, ref, acquisition)
	if err != nil || got.SHA256 != want.SHA256 {
		t.Fatalf("offline info=%+v error=%v, want digest=%s", got, err, want.SHA256)
	}
	if requests := registry.Requests(); len(requests) != 0 {
		t.Fatalf("offline requests=%v", requests)
	}
}

func TestInspectReleaseRejectsWithdrawnAndRevoked(t *testing.T) {
	t.Parallel()
	for _, state := range []pluginregistry.ReleaseState{pluginregistry.ReleaseWithdrawn, pluginregistry.ReleaseRevoked} {
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()
			registry := fixtureregistry.NewPluginRegistry(t)
			registry.Add(t, "v1.0.0", analyzerComponent)
			pin, err := registryClient(t, registry).Resolve(t.Context(), fixtureregistry.FixturePluginName, "v1.0.0")
			if err != nil {
				t.Fatal(err)
			}
			publishStatuses(t, registry, releaseStatus(pin, state))
			ref, err := pluginreference.New(pin.Name, pin.Version, fixtureregistry.FixtureRegistryURL)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pluginregistry.InspectRelease(t.Context(), ref, fixtureAcquisition(registry)); err == nil || !strings.Contains(err.Error(), string(state)) {
				t.Fatalf("inspect accepted a %s release: %v", state, err)
			}
		})
	}
}

func TestInspectReleaseRejectsCorruptDownload(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", analyzerComponent)
	pin, err := registryClient(t, registry).Resolve(t.Context(), fixtureregistry.FixturePluginName, "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	registry.SetFile(pin.URL, []byte("tampered"))
	ref, err := pluginreference.New(pin.Name, pin.Version, fixtureregistry.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pluginregistry.InspectRelease(t.Context(), ref, fixtureAcquisition(registry)); err == nil || !strings.Contains(strings.ToLower(err.Error()), "sha256") {
		t.Fatalf("inspect accepted corrupt bytes: %v", err)
	}
}
