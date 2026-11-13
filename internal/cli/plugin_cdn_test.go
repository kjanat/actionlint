package cli

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestPluginInstallHTTPSPinsAndCachesManifest(t *testing.T) {
	for _, source := range []string{
		"https://cdn.jsdelivr.net/npm/@example/plugin@1.0.0/actionlint-provider.json",
		"https://esm.sh/pr/owner/repo/@example/plugin@1234567/actionlint-provider.json?raw",
	} {
		t.Run(source, func(t *testing.T) {
			network := fixtureregistry.NewPluginRegistry(t)
			acquisition := plugininstall.Acquisition{UserCacheDir: network.UserCacheDir(), Connections: network.Connections()}
			manifest := ociProcessManifest(t, "not-installed-and-must-not-run", "[]")
			network.SetFile(source, manifest)
			root := t.TempDir()
			t.Chdir(root)
			t.Setenv("ACTIONLINT_CONFIG", "")
			t.Setenv("ACTIONLINT_NO_CONFIG", "")
			if err := os.Mkdir(".git", 0o700); err != nil {
				t.Fatal(err)
			}
			pluginCommandTest(t, acquisition, 0, "plugin", "install", source)
			pinned := fmt.Sprintf("%s#sha256=%x", source, sha256.Sum256(manifest))
			config, err := os.ReadFile(filepath.Join(".github", "actionlint.yaml"))
			if err != nil || !strings.Contains(string(config), pinned) {
				t.Fatalf("config does not pin downloaded bytes: %s, %v", config, err)
			}
			requests := len(network.Requests())
			if requests != 1 {
				t.Fatalf("installation fetched %d times, want one download", requests)
			}
			network.SetFile(source, []byte("changed upstream"))
			pluginCommandTest(t, acquisition, 0, "plugin", "install")
			if len(network.Requests()) != requests {
				t.Fatal("repeat installation fetched changed upstream bytes instead of the pinned cache")
			}
		})
	}
}
