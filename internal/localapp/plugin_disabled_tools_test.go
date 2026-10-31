package localapp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

// TestDisabledToolIsNotResolved checks that a locked plugin whose alias is
// a disabled tool id is neither downloaded nor loaded, and that the id
// counts as known.
func TestDisabledToolIsNotResolved(t *testing.T) {
	registry := testutil.NewPluginRegistry(t)
	module, err := os.ReadFile("../plugins/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry.Add(t, "v1.0.0", module)
	client, err := pluginregistry.New(testutil.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), testutil.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	pin.Alias = "no-sudo"
	lock, err := pluginregistry.OpenLock(t.Context(), filepath.Join(t.TempDir(), pluginregistry.LockName))
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Set(pin); err != nil {
		t.Fatal(err)
	}
	if err := lock.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	registry.Requests = nil
	selection := SelectionWithTools(PluginSelection{LockFile: lock.Path}, []string{"no-sudo"}, []string{"no-sudo"})
	sources, err := selection.Resolve(t.Context(), t.TempDir(), application.Input{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := CheckSources(t.Context(), application.Options{}, application.Input{
		Kind: application.Stdin, Reader: strings.NewReader("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: sudo echo hello}]\n"),
	}, sources)
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(result.Diagnostics, func(finding diagnostic.Diagnostic) bool { return finding.Rule == "no-sudo" }) {
		t.Fatalf("disabled plugin analyzed the script: %+v", result.Diagnostics)
	}
	if slices.Contains(registry.Requests, pin.URL) {
		t.Fatalf("disabled plugin was downloaded: %v", registry.Requests)
	}
}

func TestUnknownToolIDIsRejected(t *testing.T) {
	_, err := CheckSources(t.Context(), application.Options{}, application.Input{
		Kind: application.Stdin, Reader: strings.NewReader("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: echo}]\n"),
	}, PluginSources{tools: []string{"missing"}})
	unknown, ok := errors.AsType[*UnknownToolError](err)
	if !ok || !slices.Equal(unknown.IDs(), []string{"missing"}) {
		t.Fatalf("unknown tool id accepted: %v", err)
	}
}

// TestNamedPluginToolIsAccepted checks that --tool accepts the same plugin
// ids as --no-tool, here a lock pin's name and alias, and that naming a
// plugin keeps it running.
func TestNamedPluginToolIsAccepted(t *testing.T) {
	registry := testutil.NewPluginRegistry(t)
	module, err := os.ReadFile("../plugins/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry.Add(t, "v1.0.0", module)
	client, err := pluginregistry.New(testutil.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), testutil.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	pin.Alias = "sudo-check"
	lock, err := pluginregistry.OpenLock(t.Context(), filepath.Join(t.TempDir(), pluginregistry.LockName))
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Set(pin); err != nil {
		t.Fatal(err)
	}
	if err := lock.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{testutil.FixturePluginName, "sudo-check"} {
		t.Run(id, func(t *testing.T) {
			selection := SelectionWithTools(PluginSelection{LockFile: lock.Path}, nil, []string{id})
			sources, err := selection.Resolve(t.Context(), t.TempDir(), application.Input{})
			if err != nil {
				t.Fatal(err)
			}
			result, err := CheckSources(t.Context(), application.Options{}, application.Input{
				Kind: application.Stdin, Reader: strings.NewReader("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: sudo echo hello}]\n"),
			}, sources)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.ContainsFunc(result.Diagnostics, func(finding diagnostic.Diagnostic) bool { return finding.Rule == "no-sudo" }) {
				t.Fatalf("named plugin did not run: %+v", result.Diagnostics)
			}
		})
	}
}

func TestMetadataToolsReadsStaticMetadata(t *testing.T) {
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
			got, err := metadataTools(test.metadata)
			if err != nil || !slices.Equal(got, test.want) {
				t.Fatalf("metadataTools = %q, %v; want %q", got, err, test.want)
			}
		})
	}
	if _, err := metadataTools(json.RawMessage(`{`)); err == nil {
		t.Fatal("malformed metadata was accepted")
	}
}
