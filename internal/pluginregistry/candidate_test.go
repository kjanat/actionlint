package pluginregistry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

const candidateManifestPath = "plugins/no-sudo/index.json"
const candidateRecordPath = "plugins/no-sudo/versions/v1.json"

func localCandidate(t *testing.T) (string, *pluginregistry.Client) {
	t.Helper()
	network := testutil.NewPluginRegistry(t)
	data := network.Add(t, "v1.0.0", emptyComponent)
	var record struct {
		Artifact json.RawMessage `json:"artifact"`
	}
	if err := json.Unmarshal(network.File(network.RecordURL("v1.0.0")), &record); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "candidate space # +%")
	for name, data := range map[string][]byte{
		"index.json":            []byte(`{"schema_version":1,"source":"candidate","plugins":[{"name":"github.com/publisher/plugin","plugin_id":"no-sudo","manifest":"plugins/no-sudo/index.json"}]}`),
		candidateManifestPath:   []byte(`{"source":"candidate","name":"github.com/publisher/plugin","plugin_id":"no-sudo","versions":[{"version":"v1.0.0","record":"versions/v1.json"}]}`),
		candidateRecordPath:     fmt.Appendf(nil, `{"source":"candidate","artifact":%s,"url":"artifacts/plugin.wasm"}`, record.Artifact),
		"artifacts/plugin.wasm": data,
	} {
		file := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(root) + "/"}
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path
	}
	client, err := pluginregistry.New(u.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Resolve(t.Context(), testutil.FixturePluginName, "latest"); err != nil {
		t.Fatalf("valid candidate: %v", err)
	}
	t.Cleanup(func() {
		if len(network.Requests()) != 0 {
			t.Errorf("candidate accessed network: %v", network.Requests())
		}
	})
	return root, client
}

func TestCandidateRejectsInconsistentDeclarations(t *testing.T) {
	for _, tc := range []struct{ name, file, before, after string }{
		{"catalogue alias", "index.json", `"plugin_id":"no-sudo"`, `"aliases":["shellcheck"],"plugin_id":"no-sudo"`},
		{"catalogue null plugins", "index.json", `[{"name":"github.com/publisher/plugin","plugin_id":"no-sudo","manifest":"plugins/no-sudo/index.json"}]`, `null`},
		{"catalogue duplicate identity", "index.json", `}]`, `},{"name":"github.com/publisher/plugin","plugin_id":"other","manifest":"plugins/other/index.json"}]`},
		{"manifest source", candidateManifestPath, `"source":"candidate"`, `"source":"release"`},
		{"manifest repository", candidateManifestPath, `github.com/publisher/plugin`, `github.com/other/plugin`},
		{"manifest plugin", candidateManifestPath, `"plugin_id":"no-sudo"`, `"plugin_id":"other"`},
		{"manifest null versions", candidateManifestPath, `[{"version":"v1.0.0","record":"versions/v1.json"}]`, `null`},
		{"duplicate version", candidateManifestPath, `}]`, `},{"version":"v1.0.0","record":"versions/other.json"}]`},
		{"forged compatibility", candidateManifestPath, `"record":"versions/v1.json"`, `"record":"versions/v1.json","compatibility":[{"outcome":"passed"}]`},
		{"encoded manifest traversal", "index.json", `plugins/no-sudo/index.json`, `plugins/%2e%2e/no-sudo/index.json`},
		{"backslash record", candidateManifestPath, `versions/v1.json`, `versions\\v1.json`},
		{"fragment artifact", candidateRecordPath, `artifacts/plugin.wasm`, `artifacts/plugin.wasm#other`},
		{"record source", candidateRecordPath, `"source":"candidate"`, `"source":"release"`},
		{"record version", candidateRecordPath, `"plugin_version":"1.0.0"`, `"plugin_version":"2.0.0"`},
		{"metadata plugin", candidateRecordPath, `"id":"no-sudo"`, `"id":"other"`},
		{"metadata version", candidateRecordPath, `"metadata_version":1`, `"metadata_version":2`},
		{"metadata transport", candidateRecordPath, `"transport":"wasm-component"`, `"transport":"wasm-core-json"`},
		{"metadata contract", candidateRecordPath, `"interface":"kjanat:actionlint/analyzer@0.1.0"`, `"interface":"other:plugin/analyzer@0.1.0"`},
		{"unknown record field", candidateRecordPath, `"source":"candidate"`, `"source":"candidate","owner_id":42`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, client := localCandidate(t)
			file := filepath.Join(root, filepath.FromSlash(tc.file))
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			changed := bytes.Replace(data, []byte(tc.before), []byte(tc.after), 1)
			if bytes.Equal(data, changed) {
				t.Fatal("fixture mutation did not apply")
			}
			if err := os.WriteFile(file, changed, 0600); err != nil {
				t.Fatal(err)
			}
			if pin, err := client.Resolve(t.Context(), testutil.FixturePluginName, "latest"); err == nil {
				t.Fatalf("inconsistent candidate resolved: %+v", pin)
			}
		})
	}
}

func TestCandidateSearchFiltersNamesAndIDsInNameOrder(t *testing.T) {
	root, client := localCandidate(t)
	catalog := pluginregistry.Catalog{SchemaVersion: 1, Source: "candidate", Plugins: []pluginregistry.CatalogPlugin{
		{Name: "github.com/zeta/plugin", PluginID: "runner-zeta", Manifest: "plugins/zeta/index.json"},
		{Name: "github.com/runner/plugin", PluginID: "one", Manifest: "plugins/runner/index.json"},
		{Name: "github.com/alpha/plugin", PluginID: "alpha", Manifest: "plugins/alpha/index.json"},
	}}
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"", []string{"github.com/alpha/plugin", "github.com/runner/plugin", "github.com/zeta/plugin"}},
		{"RuNnEr", []string{"github.com/runner/plugin", "github.com/zeta/plugin"}},
		{"ALPHA", []string{"github.com/alpha/plugin"}},
		{"missing", nil},
	} {
		t.Run(tc.query, func(t *testing.T) {
			plugins, err := client.Search(t.Context(), tc.query)
			if err != nil {
				t.Fatal(err)
			}
			names := make([]string, len(plugins))
			for i, plugin := range plugins {
				names[i] = plugin.Name
			}
			if !slices.Equal(names, tc.want) {
				t.Fatalf("search %q: got %v, want %v", tc.query, names, tc.want)
			}
		})
	}
	if _, err := client.Resolve(t.Context(), "github.com/alpha/plug", "latest"); err == nil || !strings.Contains(err.Error(), `plugin "github.com/alpha/plug" not found`) {
		t.Fatalf("resolution accepted a partial search match: %v", err)
	}
}

func TestCandidateBoundsAndStrictJSON(t *testing.T) {
	for _, name := range []string{"index.json", candidateManifestPath, candidateRecordPath} {
		t.Run(name, func(t *testing.T) {
			root, client := localCandidate(t)
			file := filepath.Join(root, filepath.FromSlash(name))
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			padded := append(bytes.Clone(data), bytes.Repeat([]byte(" "), (4<<20)-len(data))...)
			if err := os.WriteFile(file, padded, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := client.Resolve(t.Context(), testutil.FixturePluginName, "latest"); err != nil {
				t.Fatalf("valid JSON at size limit: %v", err)
			}
			if err := os.WriteFile(file, append(padded, ' '), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := client.Resolve(t.Context(), testutil.FixturePluginName, "latest"); err == nil || !strings.Contains(err.Error(), "4 MiB") {
				t.Fatalf("oversize record: %v", err)
			}
			duplicate := bytes.Replace(data, []byte(`"source":"candidate"`), []byte(`"source":"candidate","source":"candidate"`), 1)
			if err := os.WriteFile(file, duplicate, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := client.Resolve(t.Context(), testutil.FixturePluginName, "latest"); err == nil {
				t.Fatal("duplicate JSON field accepted")
			}
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(file, 0700); err != nil {
				t.Fatal(err)
			}
			if _, err := client.Resolve(t.Context(), testutil.FixturePluginName, "latest"); err == nil {
				t.Fatal("directory accepted as candidate JSON")
			}
		})
	}
}

func TestCandidateJSONSymlinkConfinement(t *testing.T) {
	for _, name := range []string{"index.json", candidateManifestPath, candidateRecordPath} {
		t.Run(name, func(t *testing.T) {
			root, client := localCandidate(t)
			file := filepath.Join(root, filepath.FromSlash(name))
			inside := file + ".target"
			if err := os.Rename(file, inside); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Base(inside), file); err != nil {
				t.Fatal(err)
			}
			if _, err := client.Resolve(t.Context(), testutil.FixturePluginName, "latest"); err != nil {
				t.Fatalf("symlink within catalogue: %v", err)
			}
			outside := filepath.Join(t.TempDir(), "record.json")
			if err := os.Rename(inside, outside); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, file); err != nil {
				t.Fatal(err)
			}
			if _, err := client.Resolve(t.Context(), testutil.FixturePluginName, "latest"); err == nil {
				t.Fatal("candidate JSON symlink escaped catalogue")
			}
		})
	}
}

func TestCandidateCancellationAndMissingSelection(t *testing.T) {
	_, client := localCandidate(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.Search(ctx, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled catalogue search: %v", err)
	}
	if _, err := client.Resolve(ctx, testutil.FixturePluginName, "latest"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled candidate resolution: %v", err)
	}
	if _, err := client.Resolve(t.Context(), "github.com/publisher/missing", "latest"); err == nil {
		t.Fatal("missing repository resolved")
	}
	if _, err := client.Resolve(t.Context(), testutil.FixturePluginName, "v2.0.0"); err == nil {
		t.Fatal("missing version resolved")
	}
}
