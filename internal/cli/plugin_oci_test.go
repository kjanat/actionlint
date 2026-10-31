package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifact"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/testutil/ocitest"
)

// plugin install resolves an oci:// tag once, declares the digest and locks
// the blobs of every platform; inspect reads the package without running it;
// sync restores it offline from the store.
func TestPluginInstallAndInspectOCI(t *testing.T) {
	testutil.IsolateUserCache(t)
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("ACTIONLINT_CONFIG", "")
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	if err := os.Mkdir(".git", 0o700); err != nil {
		t.Fatal(err)
	}
	registry := ocitest.New(t)
	manifest := registry.Blob(pluginartifact.MediaTypeProviderManifest, []byte(`{"schema_version":1,"id":"example.com/oci-tool"}`))
	executable := registry.Blob("application/octet-stream", []byte("executable"))
	index := registry.Index(t, map[string]ocitest.Descriptor{pluginartifact.HostPlatform(): registry.Manifest(t, manifest, executable)})
	registry.Tag("v1", index)
	reference := registry.Reference(index)

	pluginCommandTest(t, 0, "plugin", "install", "oci://"+registry.Host()+"/"+ocitest.Repository+":v1")
	config, err := os.ReadFile(filepath.Join(".github", "actionlint.yaml"))
	if err != nil || !strings.Contains(string(config), reference) {
		t.Fatalf("configuration does not declare the digest %s: %q, %v", reference, config, err)
	}
	data, err := os.ReadFile(filepath.Join(".github", pluginregistry.LockName))
	if err != nil {
		t.Fatal(err)
	}
	var lock pluginregistry.Lock
	if err := json.Unmarshal(data, &lock); err != nil {
		t.Fatal(err)
	}
	pin, ok := lock.SourcePin(reference)
	if lock.SchemaVersion != pluginregistry.LockSchemaVersion || !ok {
		t.Fatalf("lock = %s", data)
	}
	if digests, ok := pin.Digests(pluginartifact.HostPlatform()); !ok || len(digests) != 4 || digests[3] != executable.Hex() {
		t.Fatalf("locked digests = %q", digests)
	}

	output := pluginCommandTest(t, 0, "plugin", "inspect", reference)
	for _, want := range []string{"Kind: manifest", "SHA256: " + index.Hex(), "Platform: " + pluginartifact.HostPlatform(), "Tool provider manifest:", "example.com/oci-tool"} {
		if !strings.Contains(output, want) {
			t.Fatalf("inspect output lacks %q:\n%s", want, output)
		}
	}
	if output := pluginCommandTest(t, 3, "plugin", "inspect", "oci://"+registry.Host()+"/"+ocitest.Repository+":v1"); !strings.Contains(output, "must pin a digest") {
		t.Fatalf("inspect accepted a tag:\n%s", output)
	}

	registry.Close()
	pluginCommandTest(t, 0, "plugin", "sync")
	if output := pluginCommandTest(t, 0, "plugin", "list"); !strings.Contains(output, reference) {
		t.Fatalf("list output lacks %s:\n%s", reference, output)
	}
}
