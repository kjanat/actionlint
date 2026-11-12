package address

import (
	"path/filepath"
	"testing"
)

func TestNormalizeLocalPercentFilename(t *testing.T) {
	name := filepath.Join(t.TempDir(), "100%preset.yaml")
	got, err := Normalize(name)
	if err != nil || got != name {
		t.Fatalf("local source = %q, %v", got, err)
	}
}

func TestNormalizeHTTPSIdentity(t *testing.T) {
	got, err := Normalize("HTTPS://EXAMPLE.TEST/base.toml?version=1")
	if err != nil || got != "https://example.test/base.toml?version=1" {
		t.Fatalf("HTTPS identity = %q, %v", got, err)
	}
}

func TestRejectInvalidSchemeAndEmptyFragment(t *testing.T) {
	for _, name := range []string{"https://example.test/a.yaml#", "https://:443/a.yaml", "http:/example.test/a.yaml", "file:/tmp/a.yaml"} {
		if got, err := Resolve(filepath.Join(t.TempDir(), "config.yaml"), name); err == nil {
			t.Errorf("accepted %q as %q", name, got)
		}
	}
}

func TestRemoteRelativeFragment(t *testing.T) {
	if got, err := Resolve("https://example.test/base.yaml", "child.yaml#"); err == nil {
		t.Fatalf("accepted fragment as %q", got)
	}
}
