package localapp

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/pluginref"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestProjectAcquisitionNeverInheritsEndpointAuthority(t *testing.T) {
	testutil.IsolateUserCache(t)
	previous := http.DefaultTransport
	dials := 0
	http.DefaultTransport = &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		dials++
		return nil, errors.New("private endpoint dialed")
	}}
	t.Cleanup(func() { http.DefaultTransport = previous })
	const endpoint = "https://127.0.0.1:8443/plugin.wasm"
	source := endpoint + "#sha256=" + strings.Repeat("a", 64)
	root := pluginRepository(t)
	path := filepath.Join(root, ".github", "actionlint.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	writeManifestTest(t, path, "plugins: ['"+source+"']\n")
	for _, selection := range []PluginSelection{{}, {ConfigFile: path}} {
		sources, err := selection.Resolve(t.Context(), root, application.Input{Kind: application.Repository})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := loadPlugins(t.Context(), sources, io.Discard, false); err == nil || !strings.Contains(err.Error(), "public destination") {
			t.Fatalf("project private source acquired: %v", err)
		}
	}
	project, err := OpenPluginProject(t.Context(), root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := pluginref.New("publisher/plugin", "v1.0.0", "https://127.0.0.1:8443/catalogue/")
	if err != nil {
		t.Fatal(err)
	}
	project.Manifest.References = []pluginref.Reference{ref}
	if _, err := project.SelectedPlugins(t.Context()); err == nil || !strings.Contains(err.Error(), "public destination") {
		t.Fatalf("project private registry acquired: %v", err)
	}
	if _, err := ScriptEngineLoader(io.Discard, false)(t.Context(), source); err == nil || !strings.Contains(err.Error(), "public destination") {
		t.Fatalf("configured backend acquired private source: %v", err)
	}
	if dials != 0 {
		t.Fatalf("automatic acquisition dialed %d times", dials)
	}
}

func TestExplicitPluginSelectionCarriesOnlyItsOwnSourceGrant(t *testing.T) {
	source := "https://127.0.0.1:8443/plugin.wasm#sha256=" + strings.Repeat("a", 64)
	sources, err := (PluginSelection{Paths: []string{source}}).Select(t.Context(), t.TempDir(), application.Input{})
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := url.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	if !sources.artifactOptions(source).Network.Allows(endpoint) {
		t.Fatal("explicit plugin source lost its authority")
	}
	other := "https://127.0.0.1:9443/plugin.wasm"
	endpoint, err = url.Parse(other)
	if err != nil {
		t.Fatal(err)
	}
	if sources.artifactOptions(source).Network.Allows(endpoint) || sources.artifactOptions(other).Network.Allows(endpoint) {
		t.Fatal("explicit source granted another acquisition authority")
	}
}
