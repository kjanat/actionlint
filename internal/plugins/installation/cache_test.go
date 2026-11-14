package installation_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestCandidateDigestCacheKeepsSameVersionBuilds(t *testing.T) {
	t.Parallel()
	root, client := localCandidate(t)
	acquisition := client.StatusOptions.Acquisition
	client.StatusOptions.CacheDir = t.TempDir()
	first, err := client.Resolve(t.Context(), "github.com/publisher/plugin", "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	options := artifacts.Options{CacheDir: filepath.Join(acquisition.UserCacheDir, "actionlint", "artifacts"), Connections: acquisition.Connections}
	statuses := plugininstall.StatusOptions{Acquisition: acquisition}
	if err := (plugininstall.ResolvedPlugins{Plugins: []plugininstall.Pin{first}}).Sync(t.Context(), options, statuses); err != nil {
		t.Fatal(err)
	}
	if err := client.RememberCandidate(t.Context(), first, options); err != nil {
		t.Fatal(err)
	}

	artifactPath := filepath.Join(root, "artifacts", "plugin.wasm")
	original, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	rebuilt := bytes.Replace(original, []byte("Test plugin"), []byte("Next plugin"), 1)
	if bytes.Equal(original, rebuilt) {
		t.Fatal("candidate rebuild did not change bytes")
	}
	secondDigest := fmt.Sprintf("%x", sha256.Sum256(rebuilt))
	if err := os.WriteFile(artifactPath, rebuilt, 0600); err != nil {
		t.Fatal(err)
	}
	recordPath := filepath.Join(root, candidateRecordPath)
	record, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recordPath, bytes.ReplaceAll(record, []byte(first.SHA256), []byte(secondDigest)), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := client.Resolve(t.Context(), first.Name, first.Version)
	if err != nil || second.SHA256 != secondDigest {
		t.Fatalf("second candidate resolution = %+v, %v", second, err)
	}
	if err := (plugininstall.ResolvedPlugins{Plugins: []plugininstall.Pin{second}}).Sync(t.Context(), options, statuses); err != nil {
		t.Fatal(err)
	}
	if err := client.RememberCandidate(t.Context(), second, options); err != nil {
		t.Fatal(err)
	}

	cold, err := plugininstall.New(first.Registry, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	cold.StatusOptions.CacheDir = t.TempDir()
	if _, err := cold.LookupCandidate(t.Context(), first.Name, first.Version, first.SHA256); err == nil || !strings.Contains(err.Error(), "changed; run actionlint plugin update") {
		t.Fatalf("cold lookup did not reject a rebuilt candidate for old digest: %v", err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []plugininstall.Pin{first, second} {
		got, err := client.LookupCandidate(t.Context(), expected.Name, expected.Version, expected.SHA256)
		if err != nil || got.SHA256 != expected.SHA256 {
			t.Fatalf("cached candidate %s lookup = %+v, %v", expected.SHA256, got, err)
		}
	}
	if _, err := cold.LookupCandidate(t.Context(), second.Name, second.Version, second.SHA256); err == nil || !strings.Contains(err.Error(), "run actionlint plugin update") {
		t.Fatalf("cold cache restored a candidate without staging bytes: %v", err)
	}
}

func TestExactRegistryLookupWorksOfflineAfterResolution(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	client, err := plugininstall.New(fixtureregistry.FixtureRegistryURL, fixtureAcquisition(registry))
	if err != nil {
		t.Fatal(err)
	}
	first, err := client.Lookup(t.Context(), fixtureregistry.FixturePluginName, "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	registry.ResetRequests()
	registry.SetOffline(true)
	second, err := client.Lookup(t.Context(), fixtureregistry.FixturePluginName, "v1.0.0")
	if err != nil || second != first || len(registry.Requests()) != 0 {
		t.Fatalf("immutable resolution was not reused offline: %+v, %v, %v", second, err, registry.Requests())
	}
	foreign, err := plugininstall.New("https://another.example/", fixtureAcquisition(registry))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := foreign.Lookup(t.Context(), fixtureregistry.FixturePluginName, "v1.0.0"); err == nil {
		t.Fatal("another registry reused the cached authority")
	}
}

func TestRegistryLookupRejectsFloatingDeclarations(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	client, err := plugininstall.New(fixtureregistry.FixtureRegistryURL, fixtureAcquisition(registry))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, version string }{{fixtureregistry.FixturePluginName, "latest"}, {"shellcheck", "v1.0.0"}} {
		if _, err := client.Lookup(t.Context(), test.name, test.version); err == nil || !strings.Contains(err.Error(), "plugin install") {
			t.Fatalf("unresolved declaration accepted: %+v, %v", test, err)
		}
	}
	if len(registry.Requests()) != 0 {
		t.Fatal("floating declarations triggered a registry lookup")
	}
}

func TestRegistryResolutionRejectsChangedImmutableVersion(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	client, err := plugininstall.New(fixtureregistry.FixtureRegistryURL, fixtureAcquisition(registry))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0, 0, 1, 0})
	registry.Publish(t, "v1.0.0")
	if _, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "v1.0.0"); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("observed release was replaced: %v", err)
	}
}
