package checking_test

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

	checking "actionlint.kjanat.dev/v2/internal/checking"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"
	pluginexec "actionlint.kjanat.dev/v2/internal/plugins/execution"

	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	"actionlint.kjanat.dev/v2/internal/development/tests/support/repositoryfixture"

	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
)

func TestProjectAcquisitionNeverInheritsEndpointAuthority(t *testing.T) {
	t.Parallel()
	dials := 0
	transport := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		dials++
		return nil, errors.New("private endpoint dialed")
	}}
	t.Cleanup(transport.CloseIdleConnections)
	acquisition := plugininstall.Acquisition{UserCacheDir: t.TempDir(), Connections: acquisitionhttp.Connections{Transport: transport}}
	const endpoint = "https://127.0.0.1:8443/plugin.wasm"
	source := endpoint + "#sha256=" + strings.Repeat("a", 64)
	root := repositoryfixture.Directory(t)
	path := filepath.Join(root, ".github", "actionlint.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	fixturefiles.WriteText(t, path, "plugins: ['"+source+"']\n")
	for _, selection := range []plugininstall.Selection{{Acquisition: acquisition}, {ConfigFile: path, Acquisition: acquisition}} {
		sources, err := selection.Resolve(t.Context(), root, inputmodel.Request{Kind: inputmodel.Repository})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pluginexec.Load(t.Context(), sources, checking.BuiltinRuleNames(), io.Discard, false); err == nil || !strings.Contains(err.Error(), "public destination") {
			t.Fatalf("project private source acquired: %v", err)
		}
	}
	project, err := plugininstall.OpenProject(t.Context(), checking.ConfigurationRules(), acquisition, root, "", "")
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
	if _, err := pluginexec.ScriptEngineLoader(acquisition, checking.BuiltinRuleNames(), io.Discard, false)(t.Context(), source); err == nil || !strings.Contains(err.Error(), "public destination") {
		t.Fatalf("configured backend acquired private source: %v", err)
	}
	if dials != 0 {
		t.Fatalf("automatic acquisition dialed %d times", dials)
	}
}

func TestExplicitPluginSelectionCarriesOnlyItsOwnSourceGrant(t *testing.T) {
	t.Parallel()
	source := "https://127.0.0.1:8443/plugin.wasm#sha256=" + strings.Repeat("a", 64)
	sources, err := (plugininstall.Selection{Paths: []string{source}, RuleSet: checking.ConfigurationRules()}).Select(t.Context(), t.TempDir(), inputmodel.Request{})
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := url.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	if !sources.ArtifactOptions(source).Network.Allows(endpoint) {
		t.Fatal("explicit plugin source lost its authority")
	}
	other := "https://127.0.0.1:9443/plugin.wasm"
	endpoint, err = url.Parse(other)
	if err != nil {
		t.Fatal(err)
	}
	if sources.ArtifactOptions(source).Network.Allows(endpoint) || sources.ArtifactOptions(other).Network.Allows(endpoint) {
		t.Fatal("explicit source granted another acquisition authority")
	}
}
