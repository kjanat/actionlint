package installation_test

import (
	"encoding/json"
	"strings"
	"testing"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
)

func TestRepositoryDeclarationCatalogueConsistency(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"gitlab.com/backend.services/plugin", "gitlab.com/Backend.Services/Team.Tools/plugin-v1",
		"gitlab.com/a/b", "github.com/owner/plugin.v1", "github.com/dotted.owner/plugin",
		"gitlab.com/.group/plugin", "gitlab.com/group./plugin", "gitlab.com/group..name/plugin",
		"gitlab.com/group.-name/plugin", "gitlab.com/group/subgroup_/plugin", "gitlab.com/group/plugin.git",
		"gitlab.com/group/plugin.atom", "gitlab.com/group/../plugin", "gitlab.com/group%2ename/plugin",
		"gitlab.com/group//plugin", "gitlab.com/group/plugin/", "gitlab.com/group/plugin?query",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := fixtureregistry.NewPluginRegistry(t)
			data, err := json.Marshal(plugininstall.Catalog{SchemaVersion: 1, Plugins: []plugininstall.CatalogPlugin{{
				Name: name, PluginID: "vendor/engine", Manifest: "plugins/" + strings.Repeat("a", 64) + "/index.json",
			}}})
			if err != nil {
				t.Fatal(err)
			}
			r.SetFile(fixtureregistry.FixtureRegistryURL+"index.json", data)
			_, declarationErr := pluginref.Parse(name)
			plugins, catalogueErr := registryClient(t, r).Search(t.Context(), "")
			if (declarationErr == nil) != (catalogueErr == nil) {
				t.Fatalf("declaration/catalogue disagree: %v / %v", declarationErr, catalogueErr)
			}
			if catalogueErr == nil && (len(plugins) != 1 || plugins[0].Name != name) {
				t.Fatalf("catalogue changed identity: %+v", plugins)
			}
		})
	}
}
