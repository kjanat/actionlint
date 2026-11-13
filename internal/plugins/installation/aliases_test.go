package installation_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
)

func TestRegistryAssignedAliases(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.SetAliases("community-check", "other-name")
	registry.Add(t, "v1.0.0", emptyComponent)
	client := registryClient(t, registry)
	for _, name := range []string{"community-check", "other-name", fixtureregistry.FixturePluginName} {
		pin, err := client.Resolve(t.Context(), name, "latest")
		if err != nil || pin.Name != fixtureregistry.FixturePluginName {
			t.Fatalf("%s: %+v %v", name, pin, err)
		}
		wantAlias := name
		if name == fixtureregistry.FixturePluginName {
			wantAlias = ""
		}
		if pin.Alias != wantAlias {
			t.Fatalf("alias=%q, want %q", pin.Alias, wantAlias)
		}
	}
	found, err := client.Search(t.Context(), "community")
	if err != nil || len(found) != 1 || found[0].Name != fixtureregistry.FixturePluginName {
		t.Fatalf("alias search: %+v %v", found, err)
	}
	versions, err := client.Versions(t.Context(), "community-check")
	if err != nil || len(versions) != 1 || versions[0].Version != "v1.0.0" {
		t.Fatalf("alias versions: %+v %v", versions, err)
	}
	if _, err := client.Resolve(t.Context(), "plugin", "latest"); err == nil {
		t.Fatal("repository basename was inferred as an alias")
	}
}

func TestRegistryRejectsAmbiguousAndInvalidAliases(t *testing.T) {
	t.Parallel()
	for _, aliases := range [][]string{{"UPPER"}, {"under_score"}, {""}, {strings.Repeat("a", 65)}, {"same", "same"}} {
		t.Run(strings.Join(aliases, ","), func(t *testing.T) {
			t.Parallel()
			registry := fixtureregistry.NewPluginRegistry(t)
			registry.SetAliases(aliases...)
			registry.Add(t, "v1.0.0", emptyComponent)
			if _, err := registryClient(t, registry).Search(t.Context(), "unrelated"); err == nil {
				t.Fatal("invalid aliases outside query accepted")
			}
		})
	}
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.SetAliases("shared")
	registry.Add(t, "v1.0.0", emptyComponent)
	var catalog plugininstall.Catalog
	if err := json.Unmarshal(registry.File(fixtureregistry.FixtureRegistryURL+"index.json"), &catalog); err != nil {
		t.Fatal(err)
	}
	other := catalog.Plugins[0]
	other.Name, other.PluginID = "github.com/another/plugin", "another"
	catalog.Plugins = append(catalog.Plugins, other)
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	registry.SetFile(fixtureregistry.FixtureRegistryURL+"index.json", data)
	if _, err := registryClient(t, registry).Versions(t.Context(), fixtureregistry.FixturePluginName); err == nil {
		t.Fatal("ambiguous alias accepted even when selecting full repository")
	}
}

func TestAliasPinIdentityAndRegistryScope(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.SetAliases("check")
	registry.Add(t, "v1.0.0", emptyComponent)
	pin, err := registryClient(t, registry).Resolve(t.Context(), "check", "latest")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := pluginref.New("check", "", fixtureregistry.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	if !pin.Matches(ref) {
		t.Fatal("recorded alias does not match")
	}
	ref, err = pluginref.New("check", "", pluginref.DefaultRegistry)
	if err != nil {
		t.Fatal(err)
	}
	if pin.Matches(ref) {
		t.Fatal("custom registry alias escaped its registry")
	}
	file, err := plugininstall.OpenLock(t.Context(), filepath.Join(t.TempDir(), plugininstall.LockName))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Set(pin); err != nil {
		t.Fatal(err)
	}
	changed := pin
	changed.Name, changed.RepositoryID = "github.com/another/plugin", "20"
	changed.URL = "https://" + changed.Name + "/releases/download/" + pin.Version + "/" + pin.Asset
	if err := file.Set(changed); err == nil || !strings.Contains(err.Error(), "alias") {
		t.Fatalf("alias reassignment accepted: %v", err)
	}
}
