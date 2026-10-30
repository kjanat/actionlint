package ruff

import (
	"encoding/json"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/tools/provider/process"
)

// The generic toml-pairs rendering of the bundled Ruff manifest must equal
// the arguments this adapter builds from the same native options.
func TestTOMLPairsMatchNativeArguments(t *testing.T) {
	for _, native := range []map[string]any{
		nil,
		{},
		{"line-length": 100, "preview": true, "unset": nil},
		{"lint": map[string]any{"select": []any{"F", "E501"}, "ignore": nil, "per-file-ignores": map[string]any{"a=b.py": []any{"F401"}}}},
		{"format": map[string]any{"quote-style": "single \"quoted\"\n"}, "indent-width": json.Number("12345678901234567890")},
		{"lint": map[string]any{"pylint": map[string]any{"max-args": 3.5}}},
		{"--fix": "--fix"},
		{"list": []any{1, nil}},
		{"nested": map[string]any{"list": []any{map[string]any{"a": nil}}}},
	} {
		want, wantErr := nativeArguments(Config{Native: native})
		encoded, err := json.Marshal(native)
		if err != nil {
			t.Fatal(err)
		}
		got, gotErr := process.TOMLPairs("--config", encoded)
		if (wantErr != nil) != (gotErr != nil) || !slices.Equal(got, want) {
			t.Errorf("%v: toml-pairs %q (%v), native arguments %q (%v)", native, got, gotErr, want, wantErr)
		}
	}
}
