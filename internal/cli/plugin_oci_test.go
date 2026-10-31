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
	executable := registry.Blob("application/octet-stream", []byte("executable"))
	manifest := registry.Blob(pluginartifact.MediaTypeProviderManifest, ociProviderManifest(t, executable))
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

// ociProviderManifest returns a process tool provider manifest whose
// executable for this host is the blob executable.
func ociProviderManifest(t *testing.T, executable ocitest.Descriptor) []byte {
	t.Helper()
	manifest, err := json.Marshal(map[string]any{
		"schema_version": 1, "id": "example.com/oci-tool", "version": "1.0.0", "role": "script", "scope": "source", "transport": "process",
		"rule":         map[string]any{"name": "oci-tool", "description": "A tool from an OCI registry", "category": "correctness", "subcategory": "shell", "maturity": "stable", "provider": "oci-tool"},
		"artifacts":    []map[string]any{{"platform": pluginartifact.HostPlatform(), "digest": executable.Digest, "filename": "oci-tool", "size": executable.Size}},
		"preparation":  "kjanat:actionlint/expression-mask@0.1.0",
		"selectors":    map[string]any{"shells": []string{"bash"}, "filename": "actionlint.sh"},
		"capabilities": map[string]any{"fixes": false, "reported_sources": false},
		"process":      map[string]any{"entry": "oci-tool", "args": []string{}, "stdin": "source", "config": map[string]any{"render": "none"}, "output": map[string]any{"format": "sarif@2.1.0"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

// config schema adds tools.<rule.name> for a provider the --plugin flag
// installs, without downloading its executable.
func TestConfigSchemaIncludesInstalledProvider(t *testing.T) {
	testutil.IsolateUserCache(t)
	t.Chdir(t.TempDir())
	t.Setenv("ACTIONLINT_CONFIG", "")
	t.Setenv("ACTIONLINT_NO_CONFIG", "1")
	registry := ocitest.New(t)
	executable := registry.Blob("application/octet-stream", []byte("executable"))
	manifest := registry.Blob(pluginartifact.MediaTypeProviderManifest, ociProviderManifest(t, executable))
	reference := registry.Reference(registry.Manifest(t, manifest, executable))

	output := pluginCommandTest(t, 0, "config", "schema", "--plugin", reference)
	var schema struct {
		Properties struct {
			Tools struct {
				OneOf []struct {
					Properties map[string]json.RawMessage `json:"properties"`
				} `json:"oneOf"`
			} `json:"tools"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(output), &schema); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, output)
	}
	found := false
	for _, alternative := range schema.Properties.Tools.OneOf {
		_, oci := alternative.Properties["oci-tool"]
		_, bundled := alternative.Properties["shellcheck"]
		found = found || oci && bundled
	}
	if !found {
		t.Fatal("tools lacks the installed provider or the bundled ones")
	}
	if got := registry.Fetches(executable); got != 0 {
		t.Fatalf("config schema downloaded the executable %d times", got)
	}
}

// plugin install refuses an OCI package whose provider manifest layer is not
// a valid tool provider manifest, and declares nothing.
func TestPluginInstallRefusesInvalidOCIProviderManifest(t *testing.T) {
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
	reference := registry.Reference(registry.Manifest(t, manifest))
	output := pluginCommandTest(t, 3, "plugin", "install", reference)
	if !strings.Contains(output, reference) || !strings.Contains(output, "invalid tool provider manifest") {
		t.Fatalf("install error does not name the source:\n%s", output)
	}
	if _, err := os.Stat(filepath.Join(".github", "actionlint.yaml")); !os.IsNotExist(err) {
		t.Fatalf("install declared an invalid provider: %v", err)
	}
}
