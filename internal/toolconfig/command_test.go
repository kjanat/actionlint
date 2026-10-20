package toolconfig

import (
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestBackendSelectsOneTransport(t *testing.T) {
	for input, want := range map[string]string{
		"process: {executable: ruff}":                  "",
		"wasm: {path: ./engines/ruff.wasm}":            "",
		"process: {executable: ruff}\nwasm: {path: a}": "backend requires exactly one of a process mapping or a wasm mapping",
		"{}":                        "backend requires exactly one of a process mapping or a wasm mapping",
		"wasm: {}":                  "wasm backend requires a module path",
		"wasm: {path: \"\"}":        "wasm backend requires a module path",
		"wasm: {path: a, extra: 1}": "extra",
	} {
		var backend Backend
		err := yaml.Unmarshal([]byte(input), &backend)
		if want == "" {
			if err != nil {
				t.Fatalf("%q: %v", input, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%q: err=%v", input, err)
		}
	}
	var backend Backend
	if err := yaml.Unmarshal([]byte("wasm: {path: ./engines/ruff.wasm}"), &backend); err != nil || backend.Process != nil || backend.Wasm.Path != "./engines/ruff.wasm" {
		t.Fatalf("backend=%+v err=%v", backend, err)
	}
}

func TestNormalizePathsBindsModulesToTheirConfig(t *testing.T) {
	root := t.TempDir()
	absolute := filepath.Join(root, "opt", "shellcheck.wasm")
	var node yaml.Node
	if err := yaml.Unmarshal([]byte("tools:\n  ruff:\n    backend:\n      wasm: {path: engines/ruff.wasm}\n  shellcheck:\n    backend:\n      wasm: {path: '"+strings.ReplaceAll(absolute, "'", "''")+"'}\noverrides:\n  - includes: ['*.yml']\n    tools:\n      ruff:\n        backend:\n          process: {executable: ./bin/ruff}\n"), &node); err != nil {
		t.Fatal(err)
	}
	NormalizePaths(&node, filepath.Join(root, ".github"))
	var out string
	if data, err := yaml.Marshal(&node); err == nil {
		out = string(data)
	} else {
		t.Fatal(err)
	}
	for _, want := range []string{filepath.Join(root, ".github", "engines", "ruff.wasm"), absolute, filepath.Join(root, ".github", "bin", "ruff")} {
		if !strings.Contains(out, want) {
			t.Fatalf("normalized config lacks %q:\n%s", want, out)
		}
	}
}
