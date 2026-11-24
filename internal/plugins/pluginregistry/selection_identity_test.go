package pluginregistry

import (
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/component"
)

func TestFallbackSelectionFollowsAdmissionIdentity(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", fixtureregistry.AnalyzerComponent())
	registry.Add(t, "v2.0.0", fixtureregistry.AnalyzerComponent())
	acquisition := dependencycache.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
	host := func(identity string) *Client {
		t.Helper()
		c, err := New(fixtureregistry.FixtureRegistryURL, acquisition)
		if err != nil {
			t.Fatal(err)
		}
		c.identity = identity
		return c
	}
	old := host("old")
	pin, err := old.ResolveCached(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil || pin.Version != "v2.0.0" {
		t.Fatalf("selection=%+v error=%v", pin, err)
	}
	if next, err := old.reselect(t.Context(), pin, &component.AdmissionError{Err: errors.New("rejected bytes")}); err != nil || next.Version != "v1.0.0" {
		t.Fatalf("reselected=%+v error=%v", next, err)
	}
	offline, _ := network.WithSession(t.Context(), network.Options{Offline: true})
	if again, err := old.ResolveCached(offline, fixtureregistry.FixturePluginName, "latest"); err != nil || again.Version != "v1.0.0" {
		t.Fatalf("same host: selection=%+v error=%v", again, err)
	}
	upgraded := host("new")
	if _, err := upgraded.ResolveCached(offline, fixtureregistry.FixturePluginName, "latest"); !network.OnlyOffline(err) {
		t.Fatalf("upgraded host reused the fallback offline: error=%v", err)
	}
	if latest, err := upgraded.ResolveCached(t.Context(), fixtureregistry.FixturePluginName, "latest"); err != nil || latest.Version != "v2.0.0" {
		t.Fatalf("upgraded host: selection=%+v error=%v", latest, err)
	}
	if again, err := upgraded.ResolveCached(offline, fixtureregistry.FixturePluginName, "latest"); err != nil || again.Version != "v2.0.0" {
		t.Fatalf("upgraded host cache: selection=%+v error=%v", again, err)
	}
	if again, err := old.ResolveCached(offline, fixtureregistry.FixturePluginName, "latest"); err != nil || again.Version != "v1.0.0" {
		t.Fatalf("old host after upgrade: selection=%+v error=%v", again, err)
	}
}
