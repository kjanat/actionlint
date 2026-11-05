package tools

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kjanat/go-yaml/v4"
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
	if err := yaml.Unmarshal([]byte("wasm: {path: ./engines/ruff.wasm}"), &backend); err != nil {
		t.Fatal(err)
	}
	module, isWasm := backend.Wasm()
	if _, isProcess := backend.Process(); isProcess || !isWasm || module.Path() != "./engines/ruff.wasm" {
		t.Fatalf("backend=%+v", backend)
	}
}

func TestBackendRoundTripsThroughYAML(t *testing.T) {
	for _, input := range []string{
		"process:\n    executable: ruff\n    args:\n        - --isolated\n    env:\n        MODE: strict\n",
		"wasm:\n    path: engines/ruff.wasm\n",
	} {
		var backend Backend
		if err := yaml.Unmarshal([]byte(input), &backend); err != nil {
			t.Fatal(err)
		}
		data, err := yaml.Marshal(&backend)
		if err != nil || string(data) != input {
			t.Fatalf("%q encoded as %q, err=%v", input, data, err)
		}
	}
}

func TestCommandOwnsItsArgumentsAndEnvironment(t *testing.T) {
	args, env := []string{"--isolated"}, map[string]string{"MODE": "strict"}
	command, err := NewCommand("ruff", args, env)
	if err != nil {
		t.Fatal(err)
	}
	args[0], env["MODE"] = "changed", "changed"
	command.Args()[0], command.Env()["MODE"] = "changed", "changed"
	if !slices.Equal(command.Args(), []string{"--isolated"}) || !maps.Equal(command.Env(), map[string]string{"MODE": "strict"}) {
		t.Fatalf("command shares storage: args=%q env=%q", command.Args(), command.Env())
	}
	for _, tc := range []struct {
		executable string
		args       []string
		env        map[string]string
	}{
		{"ru\x00ff", nil, nil},
		{"ruff", []string{"a\x00"}, nil},
		{"ruff", nil, map[string]string{"": "x"}},
		{"ruff", nil, map[string]string{"A=B": "x"}},
		{"ruff", nil, map[string]string{"A": "\x00"}},
	} {
		if _, err := NewCommand(tc.executable, tc.args, tc.env); err == nil {
			t.Fatalf("accepted %q %q %q", tc.executable, tc.args, tc.env)
		}
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

func TestNormalizePathsPreservesPluginURIs(t *testing.T) {
	for _, uri := range []string{"https://example.com/ruff.wasm.gz#sha256=abc", "file:///opt/ruff.wasm", "http://localhost:8123/ruff"} {
		var node yaml.Node
		input := "tools:\n  ruff:\n    backend:\n      wasm: {path: '" + uri + "'}\noverrides:\n  - files: ['*.yml']\n    tools:\n      ruff:\n        backend:\n          wasm: {path: '" + uri + "'}\n"
		if err := yaml.Unmarshal([]byte(input), &node); err != nil {
			t.Fatal(err)
		}
		NormalizePaths(&node, t.TempDir())
		data, err := yaml.Marshal(&node)
		if err != nil || strings.Count(string(data), "path: '"+uri+"'") != 2 {
			t.Fatalf("URI rebased: %s %v", data, err)
		}
	}
}
