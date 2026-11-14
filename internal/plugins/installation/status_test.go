package installation_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
	pluginartifacts "actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func releaseStatus(pin plugininstall.Pin, state plugininstall.ReleaseState) plugininstall.ReleaseStatus {
	return plugininstall.ReleaseStatus{
		Name: pin.Name, Version: pin.Version, SHA256: pin.SHA256, Status: state,
		Reason: "Reviewed status change", Replacement: pin.Name + "@v3.0.0",
		ChangedAt: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC),
		Actor: plugininstall.StatusActor{
			Kind: "registry-admin", ID: "123", Name: "maintainer",
			Workflow: "owner/registry/.github/workflows/status.yml@refs/heads/master", RunID: "10:1",
		},
	}
}

func publishStatuses(t *testing.T, registry *fixtureregistry.PluginRegistry, statuses ...plugininstall.ReleaseStatus) {
	t.Helper()
	data, err := json.Marshal(plugininstall.StatusSnapshot{SchemaVersion: 1, Releases: append([]plugininstall.ReleaseStatus{}, statuses...)})
	if err != nil {
		t.Fatal(err)
	}
	registry.SetFile(fixtureregistry.FixtureRegistryURL+"statuses.json", data)
}

func TestFreshSelectionExcludesWithdrawnAndRevoked(t *testing.T) {
	t.Parallel()
	for _, state := range []plugininstall.ReleaseState{plugininstall.ReleaseWithdrawn, plugininstall.ReleaseRevoked} {
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()
			registry := fixtureregistry.NewPluginRegistry(t)
			registry.Add(t, "v1.0.0", emptyComponent)
			registry.Add(t, "v2.0.0", emptyComponent)
			client := registryClient(t, registry)
			pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "v2.0.0")
			if err != nil {
				t.Fatal(err)
			}
			publishStatuses(t, registry, releaseStatus(pin, state))
			if _, err := client.Resolve(t.Context(), pin.Name, pin.Version); err == nil || !strings.Contains(err.Error(), string(state)) {
				t.Fatalf("exact selection: %v", err)
			}
			ctx, _ := network.WithSession(t.Context(), network.Options{Offline: true})
			requests := len(registry.Requests())
			if _, err := client.Resolve(ctx, pin.Name, pin.Version); err == nil || !strings.Contains(err.Error(), string(state)) {
				t.Fatalf("cached exact selection: %v", err)
			}
			if len(registry.Requests()) != requests {
				t.Fatal("offline selection contacted the registry")
			}
			selected, err := client.Resolve(t.Context(), pin.Name, "latest")
			if err != nil || selected.Version != "v1.0.0" {
				t.Fatalf("latest did not skip %s: %+v %v", state, selected, err)
			}
		})
	}
}

func TestDeprecatedSelectionWarns(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", emptyComponent)
	client := registryClient(t, registry)
	pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	publishStatuses(t, registry, releaseStatus(pin, plugininstall.ReleaseDeprecated))
	var warnings []string
	client.StatusOptions.Warning = func(message string) { warnings = append(warnings, message) }
	selected, err := client.Resolve(t.Context(), pin.Name, "latest")
	if err != nil || selected.Version != pin.Version || len(warnings) != 1 ||
		!strings.Contains(warnings[0], "deprecated") || !strings.Contains(warnings[0], "replacement:") {
		t.Fatalf("deprecation: %+v %v %v", selected, warnings, err)
	}
}

func TestLockedStatusPolicyAndPermanentCachedRevocation(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", emptyComponent)
	client := registryClient(t, registry)
	pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	lock := plugininstall.ResolvedPlugins{Plugins: []plugininstall.Pin{pin}}
	artifacts := pluginartifacts.Options{CacheDir: t.TempDir(), Connections: registry.Connections()}
	statuses := plugininstall.StatusOptions{Acquisition: fixtureAcquisition(registry)}
	if err := lock.Sync(t.Context(), artifacts, statuses); err != nil {
		t.Fatal(err)
	}
	publishStatuses(t, registry, releaseStatus(pin, plugininstall.ReleaseWithdrawn))
	if err := lock.RefreshStatuses(t.Context(), statuses); err != nil {
		t.Fatal(err)
	}
	registry.SetOffline(true)
	requests := len(registry.Requests())
	if err := lock.Sync(t.Context(), artifacts, statuses); err != nil {
		t.Fatalf("withdrawal broke existing cached lock: %v", err)
	}
	if len(registry.Requests()) != requests {
		t.Fatal("ordinary checking contacted the registry")
	}
	var warnings []string
	options := plugininstall.StatusOptions{Warning: func(message string) { warnings = append(warnings, message) }, Acquisition: fixtureAcquisition(registry)}
	if err := lock.RefreshStatuses(t.Context(), options); err != nil || len(warnings) != 2 ||
		!strings.Contains(warnings[0], "could not refresh") || !strings.Contains(warnings[1], "withdrawn") {
		t.Fatalf("offline refresh: %v %v", err, warnings)
	}
	registry.SetOffline(false)
	publishStatuses(t, registry, releaseStatus(pin, plugininstall.ReleaseRevoked))
	if err := lock.RefreshStatuses(t.Context(), options); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("revocation did not block lock: %v", err)
	}
	// A stale/empty later snapshot cannot erase known revoked bytes.
	publishStatuses(t, registry)
	if _, err := client.RefreshStatuses(t.Context()); err != nil {
		t.Fatal(err)
	}
	registry.SetOffline(true)
	requests = len(registry.Requests())
	if err := lock.Sync(t.Context(), artifacts, statuses); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("cached revoked bytes executed: %v", err)
	}
	if len(registry.Requests()) != requests {
		t.Fatal("cached revocation contacted the registry")
	}
	otherTag := pin
	otherTag.Version = "v2.0.0"
	otherTag.URL = strings.Replace(pin.URL, "/v1.0.0/", "/v2.0.0/", 1)
	lock.Plugins = []plugininstall.Pin{otherTag}
	if err := lock.CheckStatuses(t.Context(), options); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("another tag escaped digest revocation: %v", err)
	}
}

func TestStatusRequiresExactDigestAndValidSnapshot(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", emptyComponent)
	client := registryClient(t, registry)
	pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	mismatch := releaseStatus(pin, plugininstall.ReleaseWithdrawn)
	mismatch.SHA256 = strings.Repeat("f", 64)
	publishStatuses(t, registry, mismatch)
	if _, err := client.Resolve(t.Context(), pin.Name, "latest"); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("mismatched status accepted: %v", err)
	}
	for _, data := range []string{`{"schema_version":1}`, `{"schema_version":2,"releases":[]}`, `{"schema_version":1,"releases":null}`} {
		registry.SetFile(fixtureregistry.FixtureRegistryURL+"statuses.json", []byte(data))
		if _, err := client.Resolve(t.Context(), pin.Name, pin.Version); err == nil {
			t.Fatalf("invalid snapshot accepted: %s", data)
		}
	}
	registry.DeleteFile(fixtureregistry.FixtureRegistryURL + "statuses.json")
	if _, err := client.Resolve(t.Context(), pin.Name, pin.Version); err == nil {
		t.Fatal("fresh selection ignored missing status policy")
	}
}
