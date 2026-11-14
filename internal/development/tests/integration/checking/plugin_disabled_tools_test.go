package checking_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	pluginexec "actionlint.kjanat.dev/v2/internal/plugins/execution"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"

	"actionlint.kjanat.dev/v2/internal/results"
)

// TestDisabledToolIsNotResolved checks that a configured plugin whose alias is
// a disabled tool id is neither downloaded nor loaded, and that the id
// counts as known.
func TestDisabledToolIsNotResolved(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	acquisition := registryAcquisition(registry)
	module, err := os.ReadFile("../../../../plugins/execution/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry.SetAliases("no-sudo")
	registry.Add(t, "v1.0.0", module)
	client, err := plugininstall.New(fixtureregistry.FixtureRegistryURL, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), "no-sudo", "latest")
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(t.TempDir(), "actionlint.yaml")
	content := "plugins: [{name: '" + pin.Name + "', version: '" + pin.Version + "', registry: '" + pin.Registry + "'}]\n"
	if err := os.WriteFile(config, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	registry.ResetRequests()
	selection := plugininstall.WithTools(plugininstall.Selection{ConfigFile: config, RuleSet: checking.ConfigurationRules(), Acquisition: acquisition}, []string{"no-sudo"}, []string{"no-sudo"})
	sources, err := selection.Resolve(t.Context(), t.TempDir(), inputmodel.Request{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := checking.CheckWithPlugins(t.Context(), checking.Options{}, inputmodel.Request{
		Kind: inputmodel.Stdin, Reader: strings.NewReader("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: sudo echo hello}]\n"),
	}, sources)
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(result.Diagnostics, func(finding results.Diagnostic) bool { return finding.Rule == "no-sudo" }) {
		t.Fatalf("disabled plugin analyzed the script: %+v", result.Diagnostics)
	}
	if requests := registry.Requests(); slices.Contains(requests, pin.URL) {
		t.Fatalf("disabled plugin was downloaded: %v", requests)
	}
}

func TestUnknownToolIDIsRejected(t *testing.T) {
	t.Parallel()
	_, err := checking.CheckWithPlugins(t.Context(), checking.Options{}, inputmodel.Request{
		Kind: inputmodel.Stdin, Reader: strings.NewReader("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: echo}]\n"),
	}, plugininstall.Sources{NamedTools: []string{"missing"}})
	unknown, ok := errors.AsType[*checking.UnknownToolError](err)
	if !ok || !slices.Equal(unknown.IDs(), []string{"missing"}) {
		t.Fatalf("unknown tool id accepted: %v", err)
	}
}

// TestNamedPluginToolIsAccepted checks that --tool accepts the same plugin
// ids as --no-tool, here a selected release's name and alias, and that naming a
// plugin keeps it running.
func TestNamedPluginToolIsAccepted(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	acquisition := registryAcquisition(registry)
	module, err := os.ReadFile("../../../../plugins/execution/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry.SetAliases("sudo-check")
	registry.Add(t, "v1.0.0", module)
	client, err := plugininstall.New(fixtureregistry.FixtureRegistryURL, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), "sudo-check", "latest")
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(t.TempDir(), "actionlint.yaml")
	content := "plugins: [{name: '" + pin.Name + "', version: '" + pin.Version + "', registry: '" + pin.Registry + "'}]\n"
	if err := os.WriteFile(config, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{fixtureregistry.FixturePluginName, "sudo-check"} {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			selection := plugininstall.WithTools(plugininstall.Selection{ConfigFile: config, RuleSet: checking.ConfigurationRules(), Acquisition: acquisition}, nil, []string{id})
			sources, err := selection.Resolve(t.Context(), t.TempDir(), inputmodel.Request{})
			if err != nil {
				t.Fatal(err)
			}
			result, err := checking.CheckWithPlugins(t.Context(), checking.Options{}, inputmodel.Request{
				Kind: inputmodel.Stdin, Reader: strings.NewReader("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: sudo echo hello}]\n"),
			}, sources)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.ContainsFunc(result.Diagnostics, func(finding results.Diagnostic) bool { return finding.Rule == "no-sudo" }) {
				t.Fatalf("named plugin did not run: %+v", result.Diagnostics)
			}
		})
	}
}

func TestMetadataToolsReadsStaticMetadata(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		metadata json.RawMessage
		want     []string
	}{
		{"engine and rules", json.RawMessage(`{"engine":{"name":"lint-engine"},"rules":[{"name":"lint-rule"},{"name":""}]}`), []string{"lint-engine", "lint-rule"}},
		{"rules only", json.RawMessage(`{"rules":[{"name":"lint-rule"}]}`), []string{"lint-rule"}},
		{"no metadata", nil, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := pluginexec.MetadataTools(test.metadata)
			if err != nil || !slices.Equal(got, test.want) {
				t.Fatalf("metadataTools = %q, %v; want %q", got, err, test.want)
			}
		})
	}
	if _, err := pluginexec.MetadataTools(json.RawMessage(`{`)); err == nil {
		t.Fatal("malformed metadata was accepted")
	}
}
