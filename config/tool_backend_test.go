package config_test

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/config"
)

func TestPublicToolBackendsCanBeConstructed(t *testing.T) {
	backend := &config.ToolBackend{Process: &config.ToolCommand{
		Executable: "launcher", Args: []string{"--isolated"}, Env: map[string]string{"MODE": "strict"},
	}}
	constructed := config.Config{Tools: config.ToolsConfig{
		Ruff: config.RuffToolConfig{Backend: backend}, Shellcheck: config.ShellcheckToolConfig{Backend: backend},
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
	if !reflect.DeepEqual(constructed.Tools.Ruff.Backend, parsed.Tools.Ruff.Backend) ||
		!reflect.DeepEqual(constructed.Tools.Shellcheck.Backend, parsed.Tools.Shellcheck.Backend) {
		t.Fatal("public construction differs from the equivalent parsed configuration")
	}
}
