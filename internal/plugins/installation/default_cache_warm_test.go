package installation_test

import (
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
	installation "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestCacheWarmDownloadsDefaultEnginesWithoutScripts(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	root := t.TempDir()
	config := filepath.Join(root, "actionlint.yaml")
	if err := os.WriteFile(config, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	selection := installation.Selection{RuleSet: checking.ConfigurationRules(), ConfigFile: config, DefaultTools: []string{"ruff"}, Acquisition: installation.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}}
	ctx, _ := network.WithSession(t.Context(), network.Options{WarmCache: true})
	for _, version := range []string{"v1.0.0", "v1.0.1"} {
		registry.AddPlugin(t, fixtureregistry.PluginPublisher{PluginID: "example-engine", BaseURL: installation.DefaultURL, Name: "github.com/publisher/engine", Aliases: []string{"ruff"}, RepositoryID: 99}, version, []byte{0, 97, 115, 109, 1, 0, 0, 0})
		selected, err := selection.Resolve(ctx, root, inputmodel.Request{Kind: inputmodel.Repository})
		if err != nil {
			t.Fatal(err)
		}
		if selected.Releases == nil || len(selected.Releases.Plugins) != 1 {
			t.Fatalf("warm selected releases=%+v", selected.Releases)
		}
		pin := selected.Releases.Plugins[0]
		artifact := selected.Artifacts[pin.Source()]
		if pin.Version != version || artifact == nil {
			t.Fatalf("warm selected %s with artifact=%v; want %s downloaded", pin.Version, artifact, version)
		}
		if err := pin.Verify(artifact.Info); err != nil {
			t.Fatal(err)
		}
		if len(selected.LazySources) != 0 {
			t.Fatalf("warm left deferred dependencies: %v", selected.LazySources)
		}
	}
}
