package pluginregistry_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev/internal/pluginregistry"
	"actionlint.kjanat.dev/internal/plugins/artifact"
	"actionlint.kjanat.dev/internal/testutil"
)

func releaseStatus(pin pluginregistry.Pin, state pluginregistry.ReleaseState) pluginregistry.ReleaseStatus {
	return pluginregistry.ReleaseStatus{
		Name: pin.Name, Version: pin.Version, SHA256: pin.SHA256, Status: state,
		Reason: "Reviewed status change", Replacement: pin.Name + "@v3.0.0",
		ChangedAt: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC),
		Actor: pluginregistry.StatusActor{
			Kind: "registry-admin", ID: "123", Name: "maintainer",
			Workflow: "owner/registry/.github/workflows/status.yml@refs/heads/master", RunID: "10:1",
		},
	}
}

func publishStatuses(t *testing.T, registry *testutil.PluginRegistry, statuses ...pluginregistry.ReleaseStatus) {
	t.Helper()
	data, err := json.Marshal(pluginregistry.StatusSnapshot{SchemaVersion: 1, Releases: append([]pluginregistry.ReleaseStatus{}, statuses...)})
	if err != nil {
		t.Fatal(err)
	}
	registry.Files[testutil.FixtureRegistryURL+"statuses.json"] = data
}

func TestFreshSelectionExcludesWithdrawnAndRevoked(t *testing.T) {
	for _, state := range []pluginregistry.ReleaseState{pluginregistry.ReleaseWithdrawn, pluginregistry.ReleaseRevoked} {
		t.Run(string(state), func(t *testing.T) {
			registry := testutil.NewPluginRegistry(t)
			registry.Add(t, "v1.0.0", emptyComponent)
			registry.Add(t, "v2.0.0", emptyComponent)
			client := registryClient(t)
			pin, err := client.Resolve(t.Context(), testutil.FixturePluginName, "v2.0.0")
			if err != nil {
				t.Fatal(err)
			}
			publishStatuses(t, registry, releaseStatus(pin, state))
			if _, err := client.Resolve(t.Context(), pin.Name, pin.Version); err == nil || !strings.Contains(err.Error(), string(state)) {
				t.Fatalf("exact selection: %v", err)
			}
			selected, err := client.Resolve(t.Context(), pin.Name, "latest")
			if err != nil || selected.Version != "v1.0.0" {
				t.Fatalf("latest did not skip %s: %+v %v", state, selected, err)
			}
		})
	}
}

func TestDeprecatedSelectionWarns(t *testing.T) {
	registry := testutil.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", emptyComponent)
	client := registryClient(t)
	pin, err := client.Resolve(t.Context(), testutil.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	publishStatuses(t, registry, releaseStatus(pin, pluginregistry.ReleaseDeprecated))
	var warnings []string
	client.StatusOptions.Warning = func(message string) { warnings = append(warnings, message) }
	selected, err := client.Resolve(t.Context(), pin.Name, "latest")
	if err != nil || selected.Version != pin.Version || len(warnings) != 1 ||
		!strings.Contains(warnings[0], "deprecated") || !strings.Contains(warnings[0], "replacement:") {
		t.Fatalf("deprecation: %+v %v %v", selected, warnings, err)
	}
}

func TestLockedStatusPolicyAndPermanentCachedRevocation(t *testing.T) {
	registry := testutil.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", emptyComponent)
	client := registryClient(t)
	pin, err := client.Resolve(t.Context(), testutil.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	lock := pluginregistry.Lock{SchemaVersion: 1, Plugins: []pluginregistry.Pin{pin}}
	artifacts := artifact.Options{CacheDir: t.TempDir()}
	if err := lock.Sync(t.Context(), artifacts); err != nil {
		t.Fatal(err)
	}
	publishStatuses(t, registry, releaseStatus(pin, pluginregistry.ReleaseWithdrawn))
	if err := lock.RefreshStatuses(t.Context(), pluginregistry.StatusOptions{}); err != nil {
		t.Fatal(err)
	}
	registry.Offline = true
	requests := len(registry.Requests)
	if err := lock.Sync(t.Context(), artifacts); err != nil {
		t.Fatalf("withdrawal broke existing cached lock: %v", err)
	}
	if len(registry.Requests) != requests {
		t.Fatal("ordinary checking contacted the registry")
	}
	var warnings []string
	options := pluginregistry.StatusOptions{Warning: func(message string) { warnings = append(warnings, message) }}
	if err := lock.RefreshStatuses(t.Context(), options); err != nil || len(warnings) != 2 ||
		!strings.Contains(warnings[0], "could not refresh") || !strings.Contains(warnings[1], "withdrawn") {
		t.Fatalf("offline refresh: %v %v", err, warnings)
	}
	registry.Offline = false
	publishStatuses(t, registry, releaseStatus(pin, pluginregistry.ReleaseRevoked))
	if err := lock.RefreshStatuses(t.Context(), options); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("revocation did not block lock: %v", err)
	}
	// A stale/empty later snapshot cannot erase known revoked bytes.
	publishStatuses(t, registry)
	if _, err := client.RefreshStatuses(t.Context()); err != nil {
		t.Fatal(err)
	}
	registry.Offline = true
	requests = len(registry.Requests)
	if err := lock.Sync(t.Context(), artifacts); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("cached revoked bytes executed: %v", err)
	}
	if len(registry.Requests) != requests {
		t.Fatal("cached revocation contacted the registry")
	}
	otherTag := pin
	otherTag.Version = "v2.0.0"
	otherTag.URL = strings.Replace(pin.URL, "/v1.0.0/", "/v2.0.0/", 1)
	lock.Plugins = []pluginregistry.Pin{otherTag}
	if err := lock.CheckStatuses(t.Context(), options); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("another tag escaped digest revocation: %v", err)
	}
}

func TestStatusRequiresExactDigestAndValidSnapshot(t *testing.T) {
	registry := testutil.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", emptyComponent)
	client := registryClient(t)
	pin, err := client.Resolve(t.Context(), testutil.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	mismatch := releaseStatus(pin, pluginregistry.ReleaseWithdrawn)
	mismatch.SHA256 = strings.Repeat("f", 64)
	publishStatuses(t, registry, mismatch)
	if _, err := client.Resolve(t.Context(), pin.Name, "latest"); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("mismatched status accepted: %v", err)
	}
	for _, data := range []string{`{"schema_version":1}`, `{"schema_version":2,"releases":[]}`, `{"schema_version":1,"releases":null}`} {
		registry.Files[testutil.FixtureRegistryURL+"statuses.json"] = []byte(data)
		if _, err := client.Resolve(t.Context(), pin.Name, pin.Version); err == nil {
			t.Fatalf("invalid snapshot accepted: %s", data)
		}
	}
	delete(registry.Files, testutil.FixtureRegistryURL+"statuses.json")
	if _, err := client.Resolve(t.Context(), pin.Name, pin.Version); err == nil {
		t.Fatal("fresh selection ignored missing status policy")
	}
}
