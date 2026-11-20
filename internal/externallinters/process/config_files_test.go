package process

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/files"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
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
	reader := files.New(t.Context(), nil, func(path string) { recorded = append(recorded, path) })
	files := toolmanifest.ConfigFiles{Names: []string{".shellcheckrc", "shellcheckrc"}, Search: []toolmanifest.ConfigSearch{toolmanifest.ConfigSearchUpward, toolmanifest.ConfigSearchHome}}

	found, err := ConfigFile(reader, files, "", nested)
	if err != nil || found != upward || !slices.Contains(recorded, upward) {
		t.Fatalf("upward: %q %v recorded=%q", found, err, recorded)
	}
	files.Search = []toolmanifest.ConfigSearch{toolmanifest.ConfigSearchHome}
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

func TestConfigFileSearchesProjectBeforeUserConfig(t *testing.T) {
	project, userConfig := t.TempDir(), t.TempDir()
	variable := "XDG_CONFIG_HOME"
	if runtime.GOOS == "windows" {
		variable = "APPDATA"
	}
	t.Setenv(variable, userConfig)
	const name = "actionlint-test-tool.conf"
	projectFile, userFile := filepath.Join(project, name), filepath.Join(userConfig, name)
	for _, path := range []string{projectFile, userFile} {
		if err := os.WriteFile(path, []byte("tool configuration"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	config := toolmanifest.ConfigFiles{
		Names:  []string{name},
		Search: []toolmanifest.ConfigSearch{toolmanifest.ConfigSearchUpward, toolmanifest.ConfigSearchXDG},
	}
	if got, err := ConfigFile(nil, config, "", project); err != nil || got != projectFile {
		t.Fatalf("project config = %q, error = %v, want %q", got, err, projectFile)
	}
	if err := os.Remove(projectFile); err != nil {
		t.Fatal(err)
	}
	if got, err := ConfigFile(nil, config, "", project); err != nil || got != userFile {
		t.Fatalf("user config = %q, error = %v, want %q", got, err, userFile)
	}
}
