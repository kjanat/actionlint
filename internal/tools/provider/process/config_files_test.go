package process

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
)

func TestConfigFile(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path string) string {
		t.Helper()
		if err := os.WriteFile(path, []byte("disable=SC2086\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	upward := write(filepath.Join(root, "a", "shellcheckrc"))
	home := t.TempDir()
	homeFile := write(filepath.Join(home, ".shellcheckrc"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	var recorded []string
	reader := fsread.New(t.Context(), nil, func(path string) { recorded = append(recorded, path) })
	files := provider.ConfigFiles{Names: []string{".shellcheckrc", "shellcheckrc"}, Search: []provider.ConfigSearch{provider.ConfigSearchUpward, provider.ConfigSearchHome}}

	found, err := ConfigFile(reader, files, "", nested)
	if err != nil || found != upward || !slices.Contains(recorded, upward) {
		t.Fatalf("upward: %q %v recorded=%q", found, err, recorded)
	}
	files.Search = []provider.ConfigSearch{provider.ConfigSearchHome}
	// Windows tools keep per-user files in the configuration directory.
	wantHome := homeFile
	if runtime.GOOS == "windows" {
		wantHome = ""
	}
	if found, err := ConfigFile(reader, files, "", nested); err != nil || found != wantHome {
		t.Fatalf("home: %q %v, want %q", found, err, wantHome)
	}
	files.Search = nil
	if found, err := ConfigFile(reader, files, "", nested); err != nil || found != "" {
		t.Fatalf("searched without permission: %q %v", found, err)
	}
	if found, err := ConfigFile(reader, files, filepath.Join(root, "a"), nested); err != nil || found != upward {
		t.Fatalf("directory path: %q %v", found, err)
	}
	if _, err := ConfigFile(reader, files, root, nested); err == nil {
		t.Fatal("directory without configuration accepted")
	}
	if _, err := ConfigFile(reader, files, filepath.Join(root, "missing"), nested); err == nil {
		t.Fatal("missing path accepted")
	}
}
