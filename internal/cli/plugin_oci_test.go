package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/ocitest"
	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
)

func TestPluginInstallAndInspectOCI(t *testing.T) {
	acquisition := dependencycache.Acquisition{UserCacheDir: t.TempDir()}
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("ACTIONLINT_CONFIG", "")
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	if err := os.Mkdir(".git", 0o700); err != nil {
		t.Fatal(err)
	}
	registry := ocitest.New(t)
	executable := registry.Blob("application/octet-stream", []byte("executable"))
	manifest := registry.Blob(artifacts.MediaTypeProviderManifest, ociProviderManifest(t, executable))
	index := registry.Index(t, map[string]ocitest.Descriptor{artifacts.HostPlatform(): registry.Manifest(t, manifest, executable)})
	registry.Tag("v1", index)
	reference := registry.Reference(index)

	pluginCommandTest(t, acquisition, 0, "plugin", "install", "oci://"+registry.Host()+"/"+ocitest.Repository+":v1")
	config, err := os.ReadFile(filepath.Join(".github", "actionlint.yaml"))
	if err != nil || !strings.Contains(string(config), reference) {
		t.Fatalf("configuration does not declare the digest %s: %q, %v", reference, config, err)
	}
	if _, err := os.Stat(filepath.Join(".github", "actionlint.plugins.lock.json")); !os.IsNotExist(err) {
		t.Fatalf("OCI install created a project lock: %v", err)
	}
	output := pluginCommandTest(t, acquisition, 0, "plugin", "inspect", reference)
	for _, want := range []string{"Kind: manifest", "SHA256: " + index.Hex(), "Platform: " + artifacts.HostPlatform(), "Tool provider manifest:", "example.com/oci-tool"} {
		if !strings.Contains(output, want) {
			t.Fatalf("inspect output lacks %q:\n%s", want, output)
		}
	}
	if output := pluginCommandTest(t, acquisition, 3, "plugin", "inspect", "oci://"+registry.Host()+"/"+ocitest.Repository+":v1"); !strings.Contains(output, "must pin a digest") {
		t.Fatalf("inspect accepted a tag:\n%s", output)
	}

	registry.Close()
	pluginCommandTest(t, acquisition, 0, "plugin", "sync")
	if output := pluginCommandTest(t, acquisition, 0, "plugin", "list"); !strings.Contains(output, reference) {
		t.Fatalf("list output lacks %s:\n%s", reference, output)
	}
}

func ociProviderManifest(t *testing.T, executable ocitest.Descriptor) []byte {
	t.Helper()
	manifest, err := json.Marshal(map[string]any{
		"schema_version": 1, "id": "example.com/oci-tool", "version": "1.0.0", "role": "script", "scope": "source", "transport": "process",
		"rule":         map[string]any{"name": "oci-tool", "description": "A tool from an OCI registry", "category": "correctness", "subcategory": "shell", "maturity": "stable", "provider": "oci-tool"},
		"artifacts":    []map[string]any{{"platform": artifacts.HostPlatform(), "digest": executable.Digest, "filename": "oci-tool", "size": executable.Size}},
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

func TestConfigSchemaIncludesInstalledProvider(t *testing.T) {
	acquisition := dependencycache.Acquisition{UserCacheDir: t.TempDir()}
	t.Chdir(t.TempDir())
	t.Setenv("ACTIONLINT_CONFIG", "")
	t.Setenv("ACTIONLINT_NO_CONFIG", "1")
	registry := ocitest.New(t)
	executable := registry.Blob("application/octet-stream", []byte("executable"))
	manifest := registry.Blob(artifacts.MediaTypeProviderManifest, ociProviderManifest(t, executable))
	reference := registry.Reference(registry.Manifest(t, manifest, executable))

	output := pluginCommandTest(t, acquisition, 0, "config", "schema", "--plugin", reference)
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

func TestPluginInstallRefusesInvalidOCIProviderManifest(t *testing.T) {
	acquisition := dependencycache.Acquisition{UserCacheDir: t.TempDir()}
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("ACTIONLINT_CONFIG", "")
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	if err := os.Mkdir(".git", 0o700); err != nil {
		t.Fatal(err)
	}
	registry := ocitest.New(t)
	manifest := registry.Blob(artifacts.MediaTypeProviderManifest, []byte(`{"schema_version":1,"id":"example.com/oci-tool"}`))
	reference := registry.Reference(registry.Manifest(t, manifest))
	output := pluginCommandTest(t, acquisition, 3, "plugin", "install", reference)
	if !strings.Contains(output, reference) || !strings.Contains(output, "invalid tool provider manifest") {
		t.Fatalf("install error does not name the source:\n%s", output)
	}
	if _, err := os.Stat(filepath.Join(".github", "actionlint.yaml")); !os.IsNotExist(err) {
		t.Fatalf("install declared an invalid provider: %v", err)
	}
}

func TestPluginInstallValidatesProviderBeforeWritingConfig(t *testing.T) {
	acquisition := dependencycache.Acquisition{UserCacheDir: t.TempDir()}
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("ACTIONLINT_CONFIG", "")
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	if err := os.Mkdir(".git", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(".github", 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte("{\"schema_version\":1,\"plugins\":[]}\n")
	config := filepath.Join(".github", "actionlint.yaml")
	if err := os.WriteFile(config, original, 0o600); err != nil {
		t.Fatal(err)
	}
	registry := ocitest.New(t)
	manifest := registry.Blob(artifacts.MediaTypeProviderManifest, ociProcessManifest(t, "must-not-execute", "["))
	reference := registry.Reference(registry.Manifest(t, manifest))
	output := pluginCommandTest(t, acquisition, 3, "plugin", "install", reference)
	if !strings.Contains(output, reference) || !strings.Contains(output, "parse jq transform") {
		t.Fatalf("install did not report source-specific jq failure:\n%s", output)
	}
	inspectOutput := pluginCommandTest(t, acquisition, 3, "plugin", "inspect", reference)
	if !strings.Contains(inspectOutput, reference) || !strings.Contains(inspectOutput, "parse jq transform") {
		t.Fatalf("inspect did not report source-specific jq failure:\n%s", inspectOutput)
	}
	actual, err := os.ReadFile(config)
	if err != nil || !bytes.Equal(actual, original) {
		t.Fatalf("failed install changed config: %s, %v", actual, err)
	}
	if _, err := os.Stat(filepath.Join(".github", "actionlint.plugins.lock.json")); !os.IsNotExist(err) {
		t.Fatalf("failed install wrote a lock: %v", err)
	}
}

func TestPluginInstallDoesNotResolveProcessEntry(t *testing.T) {
	acquisition := dependencycache.Acquisition{UserCacheDir: t.TempDir()}
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("ACTIONLINT_CONFIG", "")
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	if err := os.Mkdir(".git", 0o700); err != nil {
		t.Fatal(err)
	}
	registry := ocitest.New(t)
	program := registry.Blob("application/vnd.actionlint.jq", []byte("[]"))
	manifest := registry.Blob(artifacts.MediaTypeProviderManifest, ociProcessManifest(t, "actionlint-install-tool-does-not-exist", "sha256:"+program.Hex()))
	reference := registry.Reference(registry.Manifest(t, manifest, program))
	output := pluginCommandTest(t, acquisition, 0, "plugin", "install", reference)
	if !strings.Contains(output, reference) {
		t.Fatalf("install output lacks source %s:\n%s", reference, output)
	}
	if count := registry.Fetches(program); count == 0 {
		t.Fatalf("digest jq program was not fetched for validation")
	}
	if _, err := os.Stat(filepath.Join(".github", "actionlint.yaml")); err != nil {
		t.Fatalf("valid provider was not installed: %v", err)
	}
}

func ociProcessManifest(t *testing.T, entry, transform string) []byte {
	t.Helper()
	transformSpec := map[string]any{"language": "jq"}
	if digest, ok := strings.CutPrefix(transform, "sha256:"); ok {
		transformSpec["digest"] = "sha256:" + digest
	} else {
		transformSpec["inline"] = transform
	}
	manifest, err := json.Marshal(map[string]any{
		"schema_version": 1, "id": "example.com/oci-authoring", "version": "1.0.0", "role": "script", "scope": "source", "transport": "process",
		"rule":         map[string]any{"name": "oci-authoring", "description": "Authoring test provider", "category": "correctness", "subcategory": "shell", "maturity": "stable", "provider": "oci-authoring"},
		"preparation":  "kjanat:actionlint/expression-mask@0.1.0",
		"selectors":    map[string]any{"shells": []string{"bash"}, "filename": "actionlint.sh"},
		"capabilities": map[string]any{"fixes": false, "reported_sources": false},
		"process": map[string]any{
			"entry": entry, "args": []string{}, "stdin": "source", "config": map[string]any{"render": "none"},
			"output": map[string]any{"format": "json", "transform": transformSpec},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}
