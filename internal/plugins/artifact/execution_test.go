package artifact

import (
	"strings"
	"testing"
)

func TestRemoteExecutionRequiresPin(t *testing.T) {
	for _, source := range []string{"https://example.com/plugin.wasm", "http://localhost/plugin.wasm"} {
		if err := RequireRemotePin(source); err == nil {
			t.Errorf("accepted unpinned execution: %s", source)
		}
	}
	for _, source := range []string{
		"./plugin.wasm",
		"https://example.com/plugin.wasm#sha256=" + strings.Repeat("ab", 32),
	} {
		if err := RequireRemotePin(source); err != nil {
			t.Errorf("%s: %v", source, err)
		}
	}
}
