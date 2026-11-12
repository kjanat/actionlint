package installation

import "testing"

func TestPinSourceLeavesLocalAndPinnedSourcesUnchanged(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"plugin.wasm",
		"plugins/100%/plugin.wasm",
		`C:\plugins\100%\plugin.wasm`,
		"file:///plugins/plugin.wasm",
		"sha256://0123456789abcdef",
		"https://example.com/plugin.wasm#sha256=0123456789abcdef",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			got, err := PinSource(t.Context(), source, Acquisition{})
			if err != nil {
				t.Fatal(err)
			}
			if got != source {
				t.Fatalf("PinSource(%q) = %q, want unchanged source", source, got)
			}
		})
	}
}
