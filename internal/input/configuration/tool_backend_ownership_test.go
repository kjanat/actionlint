package config_test

import (
	"bytes"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
)

func TestToolBackendsAreValidatedAndOwned(t *testing.T) {
	t.Parallel()
	source := []byte(`tools:
  ruff:
    backend: {process: {executable: launcher, args: [--isolated], env: {MODE: strict}}}
  shellcheck:
    backend: {wasm: {path: engines/checker.wasm}}
`)
	parsed, err := checking.ConfigurationRules().Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	before, err := checking.ConfigurationRules().Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"launcher", "--isolated", "MODE", "strict", "engines/checker.wasm"} {
		if !strings.Contains(string(before), value) {
			t.Fatalf("lost %q: %s", value, before)
		}
	}
	clear(source)
	after, err := checking.ConfigurationRules().Marshal(parsed)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("input mutation changed validated configuration: %v", err)
	}
	clear(after)
	again, err := checking.ConfigurationRules().Marshal(parsed)
	if err != nil || !bytes.Equal(before, again) {
		t.Fatalf("output mutation changed validated configuration: %v", err)
	}
	for _, source := range []string{
		"tools: {ruff: {backend: {process: {}, wasm: {path: engine.wasm}}}}",
		"tools: {ruff: {backend: {wasm: {path: ''}}}}",
		"tools: {ruff: {backend: {process: {executable: launcher, env: {'BAD=NAME': value}}}}}",
	} {
		if _, err := checking.ConfigurationRules().Parse([]byte(source)); err == nil {
			t.Fatalf("invalid backend accepted: %s", source)
		}
	}
}
