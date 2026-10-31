package component_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/plugins/component"
)

func TestDescriptorNameFitsInlineSuppressionSelector(t *testing.T) {
	data, err := os.ReadFile(pluginPath("no-sudo"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		valid bool
	}{
		{"no,sudo", false},
		{"no -- x", false},
		{"name --", false},
		{" no-sud", false},
		{"no-sud ", false},
		{"no\nsudo", false},
		{"no\rsudo", false},
		{"no-sudo", true},
		{"NO.SUDO", true},
		{"org/foo", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "plugin.wasm")
			if err := os.WriteFile(path, bytes.ReplaceAll(data, []byte("no-sudo"), []byte(test.name)), 0o600); err != nil {
				t.Fatal(err)
			}
			plugin, err := component.Load(t.Context(), path, component.Options{})
			if plugin != nil {
				defer plugin.Close(context.Background())
			}
			if (err == nil) != test.valid {
				t.Fatalf("name=%q valid=%v: %v", test.name, test.valid, err)
			}
		})
	}
}
