package localapp

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"actionlint.kjanat.dev/v2/internal/pluginref"
)

func TestPluginManifestFileMode(t *testing.T) {
	for _, mode := range []os.FileMode{0, 0o640, 0o444} {
		t.Run(fmt.Sprintf("%o", mode), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "actionlint.yml")
			control := filepath.Join(dir, "control")
			if err := os.WriteFile(control, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(control)
			if err != nil {
				t.Fatal(err)
			}
			want := info.Mode().Perm()
			if mode != 0 {
				writeManifestTest(t, path, "plugins: []\n")
				if err := os.Chmod(path, mode); err != nil {
					t.Fatal(err)
				}
				info, err = os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				want = info.Mode().Perm()
			}
			t.Cleanup(func() {
				if err := os.Chmod(path, 0o600); err != nil && !os.IsNotExist(err) {
					t.Error(err)
				}
			})
			manifest, err := OpenPluginManifest(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			err = manifest.Save(t.Context(), []pluginref.Reference{manifestTestReference(t, "new/plugin")})
			if runtime.GOOS == "windows" && mode == 0o444 {
				if err == nil || readManifestTest(t, path) != "plugins: []\n" {
					t.Fatal("read-only replacement changed original")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			info, err = os.Stat(path)
			if err != nil || info.Mode().Perm() != want {
				t.Fatalf("file mode=%v, want %o: %v", info, want, err)
			}
		})
	}
}
