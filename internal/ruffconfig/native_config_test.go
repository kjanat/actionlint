package ruffconfig

import "testing"

func TestNativeConfigReadOnlyBoundary(t *testing.T) {
	for _, key := range UnsupportedConfigOptions() {
		config := Config{Native: map[string]any{key: nil}}
		if _, err := config.NativeArguments(); err == nil {
			t.Fatalf("accepted %s", key)
		}
	}
}
