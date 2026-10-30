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
	selection := PluginSelection{LockFile: lock.Path}.WithTools([]string{"no-sudo"}, []string{"no-sudo"})
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

func TestDisabledMetadataToolReadsStaticMetadata(t *testing.T) {
	engine := json.RawMessage(`{"engine":{"name":"lint-engine"},"rules":[{"name":"lint-rule"}]}`)
	for _, test := range []struct {
		name     string
		metadata json.RawMessage
		disabled []string
		want     string
	}{
		{"engine disabled", engine, []string{"lint-engine"}, "lint-engine"},
		{"rule disabled", engine, []string{"other", "lint-rule"}, "lint-rule"},
		{"other disabled", engine, []string{"other"}, ""},
		{"nothing disabled", engine, nil, ""},
		{"no metadata", nil, []string{"lint-engine"}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := disabledMetadataTool(test.metadata, test.disabled)
			if err != nil || got != test.want {
				t.Fatalf("disabledMetadataTool = %q, %v; want %q", got, err, test.want)
			}
		})
	}
	if _, err := disabledMetadataTool(json.RawMessage(`{`), []string{"lint-engine"}); err == nil {
		t.Fatal("malformed metadata was accepted")
	}
}
