package ruff

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/analyzer"
)

func TestNativeConfigReadOnlyBoundary(t *testing.T) {
	for _, key := range unsupportedConfigOptions() {
		config := Config{Native: map[string]any{key: nil}}
		if _, err := nativeArguments(config); err == nil {
			t.Fatalf("accepted %s", key)
		}
	}
}

func TestProcessConfigCannotBecomeCommandOptions(t *testing.T) {
	for _, key := range []string{"target-version", "select", "ignore"} {
		t.Run(key, func(t *testing.T) {
			config := map[string]any{key: "--fix"}
			if key != "target-version" {
				config = map[string]any{"lint": map[string]any{key: []string{"--fix"}}}
			}
			encoded, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			called := false
			backend, _ := NewProcessBackend(func(_ context.Context, args []string, _ string) ([]byte, error) {
				called = true
				if slices.Contains(args, "--fix") || !slices.Contains(args, "--"+key+"=--fix") || !slices.Contains(args, "--no-fix") {
					t.Fatalf("configuration changed command options: %q", args)
				}
				return []byte("[]"), nil
			}, "ruff", nil, t.TempDir())
			if _, err := backend.Check(t.Context(), analyzer.ScriptRequest{Filename: stdinFilename, Source: "x", Config: encoded}); err != nil || !called {
				t.Fatalf("called=%v error=%v", called, err)
			}
		})
	}
}

func TestProcessConfigRejectsSideEffectsBeforeExecution(t *testing.T) {
	for _, key := range unsupportedConfigOptions() {
		encoded, err := json.Marshal(map[string]any{key: nil})
		if err != nil {
			t.Fatal(err)
		}
		backend, _ := NewProcessBackend(func(context.Context, []string, string) ([]byte, error) {
			t.Fatal("executed forbidden process configuration")
			return nil, nil
		}, "ruff", nil, t.TempDir())
		if _, err := backend.Check(t.Context(), analyzer.ScriptRequest{Filename: stdinFilename, Config: encoded}); err == nil || !strings.Contains(err.Error(), key) {
			t.Fatalf("%s: %v", key, err)
		}
	}
}
