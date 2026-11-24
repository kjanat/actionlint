package pluginregistry_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/component"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
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
	statuses := pluginregistry.StatusOptions{Acquisition: acquisition}
	if err := (pluginregistry.ResolvedPlugins{Plugins: []pluginregistry.Pin{first}}).Sync(t.Context(), options, statuses); err != nil {
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
	if err := os.WriteFile(artifactPath, rebuilt, 0o600); err != nil {
		t.Fatal(err)
	}
	recordPath := filepath.Join(root, candidateRecordPath)
	record, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	record = bytes.ReplaceAll(record, []byte("Test plugin"), []byte("Next plugin"))
	if err := os.WriteFile(recordPath, bytes.ReplaceAll(record, []byte(first.SHA256), []byte(secondDigest)), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := client.Resolve(t.Context(), first.Name, first.Version)
	if err != nil || second.SHA256 != secondDigest {
		t.Fatalf("second candidate resolution = %+v, %v", second, err)
	}
	if err := (pluginregistry.ResolvedPlugins{Plugins: []pluginregistry.Pin{second}}).Sync(t.Context(), options, statuses); err != nil {
		t.Fatal(err)
	}
	if err := client.RememberCandidate(t.Context(), second, options); err != nil {
		t.Fatal(err)
	}

	cold, err := pluginregistry.New(first.Registry, acquisition)
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
	for _, expected := range []pluginregistry.Pin{first, second} {
		got, err := client.LookupCandidate(t.Context(), expected.Name, expected.Version, expected.SHA256)
		if err != nil || got.SHA256 != expected.SHA256 {
			t.Fatalf("cached candidate %s lookup = %+v, %v", expected.SHA256, got, err)
		}
	}
	if _, err := cold.LookupCandidate(t.Context(), second.Name, second.Version, second.SHA256); err == nil || !strings.Contains(err.Error(), "run actionlint plugin update") {
		t.Fatalf("cold cache restored a candidate without staging bytes: %v", err)
	}
}

func TestImmutableCachesOmitSelectedRoles(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", fixtureregistry.AnalyzerComponent())
	if _, err := registryClient(t, registry).ResolveCached(t.Context(), fixtureregistry.FixturePluginName, "latest"); err != nil {
		t.Fatal(err)
	}
	registryCache, err := pluginacquisition.RegistryCacheDir(fixtureAcquisition(registry))
	if err != nil {
		t.Fatal(err)
	}
	_, candidates := localCandidate(t)
	candidates.StatusOptions.CacheDir = t.TempDir()
	pin, err := candidates.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil || pin.Roles != (component.Roles{Analyzer: component.AnalyzerInterface}) {
		t.Fatalf("latest candidate=%+v error=%v", pin, err)
	}
	acquisition := candidates.StatusOptions.Acquisition
	options := artifacts.Options{CacheDir: filepath.Join(acquisition.UserCacheDir, "actionlint", "artifacts"), Connections: acquisition.Connections}
	if err := candidates.RememberCandidate(t.Context(), pin, options); err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, root := range []string{registryCache, candidates.StatusOptions.CacheDir} {
		if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			kind := filepath.Base(filepath.Dir(path))
			if roles := bytes.Contains(data, []byte(`"roles"`)); roles != (kind == "selections") {
				t.Errorf("%s records roles=%t", path, roles)
			}
			seen[kind] = path
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if seen["releases"] == "" || seen["candidates"] == "" || seen["selections"] == "" {
		t.Fatalf("cache kinds=%v", seen)
	}
	data, err := os.ReadFile(seen["candidates"])
	if err != nil {
		t.Fatal(err)
	}
	var cached map[string]any
	if err := json.Unmarshal(data, &cached); err != nil || cached["metadata"] == nil {
		t.Fatalf("cached candidate=%s error=%v", data, err)
	}
	delete(cached, "metadata")
	if data, err = json.Marshal(cached); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(seen["candidates"]); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(seen["candidates"], data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := candidates.RememberCandidate(t.Context(), pin, options); err != nil {
		t.Fatalf("candidate cached without metadata: %v", err)
	}
}

func TestExactRegistryLookupWorksOfflineAfterResolution(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", fixtureregistry.AnalyzerComponent())
	client, err := pluginregistry.New(fixtureregistry.FixtureRegistryURL, fixtureAcquisition(registry))
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
	foreign, err := pluginregistry.New("https://another.example/", fixtureAcquisition(registry))
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
	client, err := pluginregistry.New(fixtureregistry.FixtureRegistryURL, fixtureAcquisition(registry))
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
	registry.Add(t, "v1.0.0", fixtureregistry.AnalyzerComponent())
	client, err := pluginregistry.New(fixtureregistry.FixtureRegistryURL, fixtureAcquisition(registry))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	registry.Add(t, "v1.0.0", append(fixtureregistry.AnalyzerComponent(), 0, 1, 0))
	registry.Publish(t, "v1.0.0")
	if _, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "v1.0.0"); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("observed release was replaced: %v", err)
	}
}
