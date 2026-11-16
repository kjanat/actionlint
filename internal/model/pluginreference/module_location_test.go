package pluginreference

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseModuleLocationRefusesInvalidLocations(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"", "ftp://example.com/plugin", "https://user:secret@example.com/plugin", "https://example.com/p#wat", "file://remote/plugin", "oci://example.com/plugin@sha256:" + strings.Repeat("ab", 32)} {
		if _, err := ParseModuleLocation(source); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("%s: %v", source, err)
		}
	}
}

func TestParseModuleLocationKeepsWindowsPathsAsPaths(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"C:/plugins/ruff.wasm", `C:\plugins\ruff.wasm`} {
		location, err := ParseModuleLocation(source)
		if err != nil || location.Endpoint != nil || location.Local != source {
			t.Fatalf("Windows path parsed as URI: %v %+v", err, location)
		}
	}
}

func TestModuleLocationFile(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	absolute := filepath.Join(directory, "plugins", "tool.wasm")
	pin := strings.Repeat("ab", 32)
	for _, tc := range []struct {
		source string
		want   string
	}{
		{"plugins/tool.wasm", absolute},
		{filepath.Join(directory, "plugins") + string(filepath.Separator) + "." + string(filepath.Separator) + "tool.wasm", absolute},
		{"https://example.com/tool.wasm#sha256=" + strings.ToUpper(pin), ""},
		{"sha256://" + pin, ""},
	} {
		location, err := ParseModuleLocation(tc.source)
		if err != nil {
			t.Fatalf("%s: %v", tc.source, err)
		}
		if got := location.File(directory); got != tc.want {
			t.Errorf("%s: File = %q, want %q", tc.source, got, tc.want)
		}
	}
	location, err := ParseModuleLocation("https://example.com/tool.wasm#sha256=" + strings.ToUpper(pin))
	if err != nil || location.Pin != pin || location.Endpoint.Fragment != "" {
		t.Fatalf("pin = %+v, %v", location, err)
	}
}
