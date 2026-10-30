package ruff

import (
	"encoding/json"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/internal/tools/provider/process"
)

// The bundled Ruff manifest must forbid exactly the configuration keys and
// unset exactly the environment this adapter does, so the manifest route
// keeps the adapter's read-only boundary when it replaces the adapter. The
// process package checks that the transport enforces every listed key.
func TestManifestBoundaryMatchesAdapter(t *testing.T) {
	manifests, err := provider.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	index := slices.IndexFunc(manifests, func(m provider.Manifest) bool { return m.Rule.Name == "ruff" })
	if index < 0 || manifests[index].Process == nil {
		t.Fatal("no bundled Ruff process manifest")
	}
	spec := manifests[index].Process
	for _, pair := range []struct {
		field             string
		manifest, adapter []string
	}{
		{"process.config.forbidden", spec.Config.Forbidden, unsupportedConfigOptions()},
		{"process.env_unset", spec.EnvUnset, UnsetEnvironment()},
	} {
		if got, want := slices.Sorted(slices.Values(pair.manifest)), slices.Sorted(slices.Values(pair.adapter)); !slices.Equal(got, want) {
			t.Errorf("%s %q, adapter %q", pair.field, got, want)
		}
	}
}

// The generic toml-pairs rendering of the bundled Ruff manifest must equal
// the arguments this adapter builds from the same native options.
func TestTOMLPairsMatchNativeArguments(t *testing.T) {
	for _, native := range []map[string]any{
		nil,
		{},
		{"line-length": 100, "preview": true, "unset": nil},
		{"lint": map[string]any{"select": []any{"F", "E501"}, "ignore": nil, "per-file-ignores": map[string]any{"a=b.py": []any{"F401"}}}},
		{"format": map[string]any{"quote-style": "single \"quoted\"\n"}, "indent-width": json.Number("12345678901234567890")},
		{"lint": map[string]any{"pylint": map[string]any{"max-args": 3.5}}},
		{"--fix": "--fix"},
		{"list": []any{1, nil}},
		{"nested": map[string]any{"list": []any{map[string]any{"a": nil}}}},
	} {
		want, wantErr := nativeArguments(Config{Native: native})
		encoded, err := json.Marshal(native)
		if err != nil {
			t.Fatal(err)
		}
		got, gotErr := process.TOMLPairs("--config", encoded)
		if (wantErr != nil) != (gotErr != nil) || !slices.Equal(got, want) {
			t.Errorf("%v: toml-pairs %q (%v), native arguments %q (%v)", native, got, gotErr, want, wantErr)
		}
	}
}
