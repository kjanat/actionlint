package config_test

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/config"
)

func TestPublicToolBackendsCanBeConstructed(t *testing.T) {
	command, err := config.NewToolCommand("launcher", []string{"--isolated"}, map[string]string{"MODE": "strict"})
	if err != nil {
		t.Fatal(err)
	}
	backend := config.ToolProcessBackend(command)
	constructed := config.Config{Tools: config.ToolsConfig{
		"ruff": {Backend: backend}, "shellcheck": {Backend: backend},
	}}
	module, err := config.NewToolModule("engines/ruff.wasm")
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := config.ToolWasmBackend(module).Wasm(); !ok || got.Path() != "engines/ruff.wasm" {
		t.Fatalf("wasm backend selects %+v, ok=%t", got, ok)
	}
	parsed, err := config.Parse([]byte(`tools:
  ruff:
    backend: {process: {executable: launcher, args: [--isolated], env: {MODE: strict}}}
  shellcheck:
    backend: {process: {executable: launcher, args: [--isolated], env: {MODE: strict}}}
`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(constructed.Tools["ruff"].Backend, parsed.Tools["ruff"].Backend) ||
		!reflect.DeepEqual(constructed.Tools["shellcheck"].Backend, parsed.Tools["shellcheck"].Backend) {
		t.Fatal("public construction differs from the equivalent parsed configuration")
	}
}
