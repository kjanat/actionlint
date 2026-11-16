package manifests_test

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/bundledproviders"
	"actionlint.kjanat.dev/v2/internal/model/manifests"
	"actionlint.kjanat.dev/v2/internal/model/toolsettings"
)

// Bundled decodes once, so a caller that edits its result, as tests that
// derive a provider from shellcheck do, must not change what later callers get.
func TestBundledReturnsCallerOwnedManifests(t *testing.T) {
	t.Parallel()
	edited, err := manifests.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	if len(edited) == 0 {
		t.Fatal("no bundled providers")
	}
	for i := range edited {
		manifest := &edited[i]
		manifest.Rule.Name = "edited"
		if len(manifest.Rule.Aliases) != 0 {
			manifest.Rule.Aliases[0].Name = "edited"
		}
		for key := range manifest.ConfigShorthands {
			manifest.ConfigShorthands[key] = toolsettings.Shorthand{}
		}
		manifest.ConfigShorthands = nil
		if manifest.Process != nil {
			if len(manifest.Process.Args) != 0 {
				manifest.Process.Args[0] = "edited"
			}
			manifest.Process.Args = nil
			manifest.Process.Output.Format = ""
			manifest.Process.Entry = "edited"
		}
	}
	got, err := manifests.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	names := bundledproviders.Names()
	if len(got) != len(names) {
		t.Fatalf("got %d manifests, want %d", len(got), len(names))
	}
	for i, name := range names {
		data, _ := bundledproviders.Manifest(name)
		want, err := manifests.ParsePluginManifest(data)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got[i], want) {
			t.Errorf("%s: Bundled returned a manifest changed by an earlier caller", name)
		}
	}
}
