package plugins

import (
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
)

func TestReferencesErrorPaths(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		value any
		path  string
	}{
		{"invalid list", true, "/plugins"},
		{"invalid entry", []any{42}, "/plugins/0"},
		{"unknown field", []any{map[string]any{"unknown": "value"}}, "/plugins/0/unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, declared, err := References(map[string]any{"plugins": tc.value})
			failure, ok := errors.AsType[*binding.Error](err)
			if !declared || !ok || failure.Path != tc.path {
				t.Fatalf("declared=%t error=%v, want path %s", declared, err, tc.path)
			}
		})
	}
}
