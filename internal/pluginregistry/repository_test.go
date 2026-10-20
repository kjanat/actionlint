package pluginregistry_test

import (
	"encoding/json"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/pluginref"
	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestRepositoryDeclarationCatalogueConsistency(t *testing.T) {
	for _, name := range []string{
		"gitlab.com/backend.services/plugin", "gitlab.com/Backend.Services/Team.Tools/plugin-v1",
		"gitlab.com/a/b", "github.com/owner/plugin.v1", "github.com/dotted.owner/plugin",
		"gitlab.com/.group/plugin", "gitlab.com/group./plugin", "gitlab.com/group..name/plugin",
		"gitlab.com/group.-name/plugin", "gitlab.com/group/subgroup_/plugin", "gitlab.com/group/plugin.git",
		"gitlab.com/group/plugin.atom", "gitlab.com/group/../plugin", "gitlab.com/group%2ename/plugin",
		"gitlab.com/group//plugin", "gitlab.com/group/plugin/", "gitlab.com/group/plugin?query",
	} {
		t.Run(name, func(t *testing.T) {
			r := testutil.NewPluginRegistry(t)
			data, err := json.Marshal(pluginregistry.Catalog{SchemaVersion: 1, Plugins: []pluginregistry.CatalogPlugin{{
				Name: name, PluginID: "vendor/engine", Manifest: "plugins/" + strings.Repeat("a", 64) + "/index.json",
			}}})
			if err != nil {
				t.Fatal(err)
			}
			r.Files[testutil.FixtureRegistryURL+"index.json"] = data
			_, declarationErr := pluginref.Parse(name)
			plugins, catalogueErr := registryClient(t).Search(t.Context(), "")
			if (declarationErr == nil) != (catalogueErr == nil) {
				t.Fatalf("declaration/catalogue disagree: %v / %v", declarationErr, catalogueErr)
			}
			if catalogueErr == nil && (len(plugins) != 1 || plugins[0].Name != name) {
				t.Fatalf("catalogue changed identity: %+v", plugins)
			}
		})
	}
}
