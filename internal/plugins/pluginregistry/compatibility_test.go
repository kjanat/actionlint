package pluginregistry_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sha256hex"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/wasmcomponent"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/component"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
)

var futureComponent = wasmcomponent.Build(nil, []string{"kjanat:actionlint/analyzer@99.0.0"}, nil)

func artifactURL(tag string) string {
	return "https://" + fixtureregistry.FixturePluginName + "/releases/download/" + tag + "/plugin.wasm"
}

func TestLatestSkipsReleasesListedIncompatible(t *testing.T) {
	t.Parallel()
	r := fixtureregistry.NewPluginRegistry(t)
	r.Add(t, "v1.0.0", analyzerComponent)
	r.Add(t, "v2.0.0", futureComponent)
	client := registryClient(t, r)
	pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil || pin.Version != "v1.0.0" {
		t.Fatalf("latest=%+v error=%v", pin, err)
	}
	if slices.Contains(r.Requests(), artifactURL("v2.0.0")) {
		t.Fatal("listed interfaces did not spare the incompatible download")
	}
	exact, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "v2.0.0")
	if err != nil {
		t.Fatalf("exact selector resolution: %v", err)
	}
	err = pluginregistry.Sync(t.Context(), pluginregistry.ResolvedPlugins{Plugins: []pluginregistry.Pin{exact}}, fixtureAcquisition(r))
	if incompatible, ok := errors.AsType[*pluginregistry.IncompatibleError](err); !ok || incompatible.Version != "v2.0.0" || !strings.Contains(err.Error(), "this host supports "+component.AnalyzerInterface) {
		t.Fatalf("sync accepted an incompatible release: %v", err)
	}
}

func TestLatestChecksBytesWithoutInterfaceLists(t *testing.T) {
	t.Parallel()
	r := fixtureregistry.NewPluginRegistry(t)
	r.OmitInterfaces()
	r.Add(t, "v1.0.0", analyzerComponent)
	r.Add(t, "v2.0.0", futureComponent)
	pin, err := registryClient(t, r).Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil || pin.Version != "v1.0.0" {
		t.Fatalf("latest=%+v error=%v", pin, err)
	}
	if !slices.Contains(r.Requests(), artifactURL("v2.0.0")) {
		t.Fatal("unlisted release was selected without reading its bytes")
	}
}

func TestLatestSkipsUnreadableComponents(t *testing.T) {
	t.Parallel()
	r := fixtureregistry.NewPluginRegistry(t)
	r.Add(t, "v1.0.0", analyzerComponent)
	r.Add(t, "v2.0.0", append(fixtureregistry.AnalyzerComponent(), 11, 1, 5))
	pin, err := registryClient(t, r).Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil || pin.Version != "v1.0.0" || !slices.Contains(r.Requests(), artifactURL("v2.0.0")) {
		t.Fatalf("latest=%+v error=%v", pin, err)
	}
}

func TestLatestWithoutInterfaceListsReportsDownloadFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		edit func(*fixtureregistry.PluginRegistry)
		want string
	}{
		{"missing artifact", func(r *fixtureregistry.PluginRegistry) { r.DeleteFile(artifactURL("v1.0.0")) }, "HTTP 404"},
		{"metadata mismatch", func(r *fixtureregistry.PluginRegistry) {
			url := r.RecordURL("v1.0.0")
			r.SetFile(url, bytes.ReplaceAll(r.File(url), []byte(`"name":"Test plugin"`), []byte(`"name":"Other plugin"`)))
		}, "artifact metadata disagrees"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := fixtureregistry.NewPluginRegistry(t)
			r.OmitInterfaces()
			r.Add(t, "v1.0.0", analyzerComponent)
			tc.edit(r)
			_, err := registryClient(t, r).Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
			if err == nil || !strings.Contains(err.Error(), tc.want) || errors.Is(err, pluginregistry.ErrNoCompatibleRelease) {
				t.Fatalf("error=%v, want %q", err, tc.want)
			}
		})
	}
}

func TestLatestWithoutCompatibleRelease(t *testing.T) {
	t.Parallel()
	r := fixtureregistry.NewPluginRegistry(t)
	r.Add(t, "v1.0.0", futureComponent)
	client := registryClient(t, r)
	_, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	want := fixtureregistry.FixturePluginName + ": " + pluginregistry.ErrNoCompatibleRelease.Error() + "\nplugin " + fixtureregistry.FixturePluginName + "@v1.0.0 does not load in this actionlint: "
	if !errors.Is(err, pluginregistry.ErrNoCompatibleRelease) || !strings.HasPrefix(err.Error(), want) || strings.Count(err.Error(), "\n") != 1 {
		t.Fatalf("error=%v", err)
	}
	r.Add(t, "v0.9.0", analyzerComponent)
	r.Add(t, "0.9.0", analyzerComponent)
	_, err = client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if !errors.Is(err, pluginregistry.ErrNoCompatibleRelease) || !strings.HasSuffix(err.Error(), "\nlatest version has ambiguous tags; select an exact tag with --version") {
		t.Fatalf("error with ambiguous older tags=%v", err)
	}
}

func TestLatestChecksComponentTypes(t *testing.T) {
	t.Parallel()
	r := fixtureregistry.NewPluginRegistry(t)
	r.OmitInterfaces()
	r.Add(t, "v1.0.0", analyzerComponent)
	r.Add(t, "v2.0.0", wasmcomponent.Build(nil, []string{component.AnalyzerInterface}, nil))
	client := registryClient(t, r)
	pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil || pin.Version != "v1.0.0" {
		t.Fatalf("latest=%+v error=%v", pin, err)
	}
	exact, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "v2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	err = pluginregistry.Sync(t.Context(), pluginregistry.ResolvedPlugins{Plugins: []pluginregistry.Pin{exact}}, fixtureAcquisition(r))
	if _, ok := errors.AsType[*pluginregistry.IncompatibleError](err); !ok || !strings.Contains(err.Error(), "incompatible analyzer contract") {
		t.Fatalf("sync accepted an analyzer without its functions: %v", err)
	}
}

func TestLatestStaysOnStableReleases(t *testing.T) {
	t.Parallel()
	r := fixtureregistry.NewPluginRegistry(t)
	r.Add(t, "v1.0.0", futureComponent)
	r.Add(t, "v2.0.0-rc.1", analyzerComponent)
	client := registryClient(t, r)
	if pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest"); !errors.Is(err, pluginregistry.ErrNoCompatibleRelease) {
		t.Fatalf("latest fell back to a prerelease: %+v %v", pin, err)
	}
	r = fixtureregistry.NewPluginRegistry(t)
	r.Add(t, "v1.0.0", analyzerComponent)
	r.Add(t, "v2.0.0-rc.1", analyzerComponent)
	client = registryClient(t, r)
	pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	publishStatuses(t, r, releaseStatus(pin, pluginregistry.ReleaseWithdrawn))
	if pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest"); err == nil || !strings.Contains(err.Error(), "no published stable release is available") {
		t.Fatalf("latest fell back to a prerelease after withdrawal: %+v %v", pin, err)
	}
}

func TestLatestCandidateSkipsIncompatibleRelease(t *testing.T) {
	t.Parallel()
	network := fixtureregistry.NewPluginRegistry(t)
	root := filepath.Join(t.TempDir(), "candidates")
	files := map[string][]byte{
		"index.json":                 []byte(`{"schema_version":1,"source":"candidate","plugins":[{"name":"github.com/publisher/plugin","plugin_id":"no-sudo","manifest":"plugins/no-sudo/index.json"}]}`),
		"plugins/no-sudo/index.json": []byte(`{"source":"candidate","name":"github.com/publisher/plugin","plugin_id":"no-sudo","versions":[{"version":"v1.0.0","record":"versions/v1.json"},{"version":"v2.0.0","record":"versions/v2.json"}]}`),
	}
	for tag, wasm := range map[string][]byte{"v1": analyzerComponent, "v2": futureComponent} {
		data := network.Add(t, tag+".0.0", wasm)
		var record struct {
			Artifact json.RawMessage `json:"artifact"`
		}
		if err := json.Unmarshal(network.File(network.RecordURL(tag+".0.0")), &record); err != nil {
			t.Fatal(err)
		}
		files["plugins/no-sudo/versions/"+tag+".json"] = fmt.Appendf(nil, `{"source":"candidate","artifact":%s,"url":"artifacts/%s.wasm"}`, record.Artifact, tag)
		files["artifacts/"+tag+".wasm"] = data
	}
	for name, data := range files {
		file := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	client, err := pluginregistry.New((&url.URL{Scheme: "file", Path: filepath.ToSlash(root) + "/"}).String(), fixtureAcquisition(network))
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil || pin.Version != "v1.0.0" || pin.Origin != "candidate" {
		t.Fatalf("latest candidate=%+v error=%v", pin, err)
	}
}

func TestSelectionCacheBelongsToAcceptedInterfaces(t *testing.T) {
	t.Parallel()
	r := fixtureregistry.NewPluginRegistry(t)
	r.Add(t, "v1.0.0", analyzerComponent)
	client := registryClient(t, r)
	if _, err := client.ResolveCached(t.Context(), fixtureregistry.FixturePluginName, "latest"); err != nil {
		t.Fatal(err)
	}
	cache, err := pluginacquisition.RegistryCacheDir(fixtureAcquisition(r))
	if err != nil {
		t.Fatal(err)
	}
	selections := filepath.Join(cache, sha256hex.Digest([]byte(fixtureregistry.FixtureRegistryURL)), "selections")
	current := filepath.Join(selections, sha256hex.Digest([]byte(fixtureregistry.FixturePluginName+"\nlatest\n"+component.AcceptedInterfacesDigest()))+".json")
	previousHost := filepath.Join(selections, sha256hex.Digest([]byte(fixtureregistry.FixturePluginName+"\nlatest\n"+strings.Repeat("0", 64)))+".json")
	if err := os.Rename(current, previousHost); err != nil {
		t.Fatal(err)
	}
	ctx, _ := network.WithSession(t.Context(), network.Options{Offline: true})
	if _, err := client.ResolveCached(ctx, fixtureregistry.FixturePluginName, "latest"); !errors.Is(err, network.ErrOffline) {
		t.Fatalf("selection of another host was reused: %v", err)
	}
}
