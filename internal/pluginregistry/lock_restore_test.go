package pluginregistry_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/internal/pluginregistry"
)

func TestLockRestore(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "existing"}[existing], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), pluginregistry.LockName)
			original := []byte("{\"schema_version\":1,\"plugins\":[]}\n")
			if existing {
				if err := os.WriteFile(path, original, 0600); err != nil {
					t.Fatal(err)
				}
			}
			file, err := pluginregistry.OpenLock(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			previous := *file
			if err := file.Save(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := file.Restore(t.Context(), &previous); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if existing && (err != nil || !bytes.Equal(data, original)) {
				t.Fatalf("original bytes not restored: %s %v", data, err)
			}
			if !existing && !os.IsNotExist(err) {
				t.Fatalf("new lock left behind: %v", err)
			}
			if err := file.Save(t.Context()); err != nil {
				t.Fatal(err)
			}
			concurrent := []byte("{\"schema_version\":1, \"plugins\":[]}")
			if err := os.WriteFile(path, concurrent, 0600); err != nil {
				t.Fatal(err)
			}
			if err := file.Restore(t.Context(), &previous); err == nil {
				t.Fatal("rollback overwrote another writer")
			}
			data, err = os.ReadFile(path)
			if err != nil || !bytes.Equal(data, concurrent) {
				t.Fatalf("concurrent write lost: %s %v", data, err)
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil || len(entries) != 1 {
				t.Fatalf("transaction debris: %v %v", entries, err)
			}
		})
	}
}
