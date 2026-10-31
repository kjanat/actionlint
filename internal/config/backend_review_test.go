package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestInheritedBackendSelectionReplacesTransport(t *testing.T) {
	for _, tool := range []string{"ruff", "shellcheck"} {
		for _, selection := range []struct{ base, child string }{
			{"process: {executable: checker}", "wasm: {path: checker.wasm}"},
			{"wasm: {path: checker.wasm}", "process: {executable: checker}"},
		} {
			t.Run(tool+selection.child, func(t *testing.T) {
				root := t.TempDir()
				base := filepath.Join(root, "base.yml")
				child := filepath.Join(root, "child.yml")
				for path, content := range map[string]string{base: fmt.Sprintf("tools: {%s: {backend: {%s}}}", tool, selection.base), child: fmt.Sprintf("extends: [base.yml]\ntools: {%s: {backend: {%s}}}", tool, selection.child)} {
					if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				loaded, err := Read(child, os.ReadFile)
				if err != nil {
					t.Fatal(err)
				}
				backend := loaded.Config.Tools["ruff"].Backend
				if tool == "shellcheck" {
					backend = loaded.Config.Tools["shellcheck"].Backend
				}
				_, process := backend.Process()
				_, wasm := backend.Wasm()
				if process == wasm {
					t.Fatalf("backend=%+v", backend)
				}
			})
		}
	}
}
