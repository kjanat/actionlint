package config_test

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/config"
)

func TestPublicToolBackendsCanBeConstructed(t *testing.T) {
	backend := &config.ToolBackend{Process: &config.ToolCommand{
		Executable: "launcher", Args: []string{"--isolated"}, Env: map[string]string{"MODE": "strict"},
	}}
	constructed := config.Config{Tools: config.ToolsConfig{
		"ruff": {Backend: backend}, "shellcheck": {Backend: backend},
	}}
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
