package pluginregistry_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestExactRegistryLookupWorksOfflineAfterResolution(t *testing.T) {
	registry := testutil.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	client, err := pluginregistry.New(testutil.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	first, err := client.Lookup(t.Context(), testutil.FixturePluginName, "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	registry.ResetRequests()
	registry.SetOffline(true)
	second, err := client.Lookup(t.Context(), testutil.FixturePluginName, "v1.0.0")
	if err != nil || second != first || len(registry.Requests()) != 0 {
		t.Fatalf("immutable resolution was not reused offline: %+v, %v, %v", second, err, registry.Requests())
	}
	foreign, err := pluginregistry.New("https://another.example/")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := foreign.Lookup(t.Context(), testutil.FixturePluginName, "v1.0.0"); err == nil {
		t.Fatal("another registry reused the cached authority")
	}
}

func TestRegistryLookupRejectsFloatingDeclarations(t *testing.T) {
	registry := testutil.NewPluginRegistry(t)
	client, err := pluginregistry.New(testutil.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, version string }{{testutil.FixturePluginName, "latest"}, {"shellcheck", "v1.0.0"}} {
		if _, err := client.Lookup(t.Context(), test.name, test.version); err == nil || !strings.Contains(err.Error(), "plugin install") {
			t.Fatalf("unresolved declaration accepted: %+v, %v", test, err)
		}
	}
	if len(registry.Requests()) != 0 {
		t.Fatal("floating declarations triggered a registry lookup")
	}
}

func TestRegistryResolutionRejectsChangedImmutableVersion(t *testing.T) {
	registry := testutil.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	client, err := pluginregistry.New(testutil.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Resolve(t.Context(), testutil.FixturePluginName, "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0, 0, 1, 0})
	registry.Publish(t, "v1.0.0")
	if _, err := client.Resolve(t.Context(), testutil.FixturePluginName, "v1.0.0"); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("observed release was replaced: %v", err)
	}
}
