package configuration

import (
	"reflect"
	"slices"
	"testing"

	pluginmanifests "actionlint.kjanat.dev/v2/internal/model/manifests"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

// Configuration indexes the validated bundled manifests; each entry keeps
// its manifest's scope, config files, schema and shorthands.
func TestToolProvidersMatchManifests(t *testing.T) {
	t.Parallel()
	manifests, err := pluginmanifests.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, manifest := range manifests {
		name := manifest.Rule.Name
		names = append(names, name)
		tool, ok := toolProviders()[name]
		if !ok {
			t.Fatalf("configuration does not know bundled provider %s", name)
		}
		scopes := map[pluginmanifests.Scope]rule.ConfigurationScope{pluginmanifests.ScopeComposite: rule.CompositeConfiguration, pluginmanifests.ScopeSource: rule.SourceConfiguration}
		if tool.scope != scopes[manifest.Scope] {
			t.Errorf("%s: scope %v, manifest %q", name, tool.scope, manifest.Scope)
		}
		if tool.configFiles != manifest.ReadsConfigFiles() {
			t.Errorf("%s: config files %v, manifest %v", name, tool.configFiles, manifest.ReadsConfigFiles())
		}
		if (tool.schema != nil) != (len(manifest.ConfigSchema) != 0) {
			t.Errorf("%s: schema compiled %v, manifest declares one %v", name, tool.schema != nil, len(manifest.ConfigSchema) != 0)
		}
		if !reflect.DeepEqual(tool.shorthands, manifest.ConfigShorthands) {
			t.Errorf("%s: shorthands %+v, manifest %+v", name, tool.shorthands, manifest.ConfigShorthands)
		}
	}
	if !slices.Equal(toolProviderNames(), names) {
		t.Fatalf("configuration providers %v, manifests %v", toolProviderNames(), names)
	}
}
