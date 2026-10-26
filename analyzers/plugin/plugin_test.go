package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContractIsRequired(t *testing.T) {
	for _, settings := range []any{nil, map[string]any{}, map[string]any{"contract": "missing.json"}, map[string]any{"contrcat": "../../architecture.json"}} {
		if _, err := newPlugin(settings); err == nil {
			t.Fatalf("accepted missing/invalid contract: %#v", settings)
		}
	}
	p, err := newPlugin(map[string]any{"contract": "../../architecture.json"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := p.BuildAnalyzers()
	if err != nil || len(a) != 1 {
		t.Fatalf("plugin did not load analyzer: %v", err)
	}
}

func TestWrongModuleIsRejected(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "architecture.json")
	if err := os.WriteFile(filename, []byte(`{"module":"example.org/wrong","layers":[{"name":"values","roots":["values"],"uses":[]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newPlugin(map[string]any{"contract": filename}); err == nil {
		t.Fatal("accepted another repository's contract")
	}
}
