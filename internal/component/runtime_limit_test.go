package component_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/partite-ai/wacogo"
)

// The canonical ABI checks only alignment and bounds on a realloc result, so a
// null pointer is valid for a zero-size request, which TinyGo's cabi_realloc
// returns. Upstream wacogo traps on it (partite-ai/wacogo#11); go.mod replaces
// the module with the fix from partite-ai/wacogo#12 until that merges.
func TestRuntimeAcceptsNullReallocForZeroSize(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "wat", "null-realloc.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	engine := wacogo.NewEngine(t.Context())
	defer func() { _ = engine.Close(t.Context()) }()
	component, err := engine.LoadComponent(t.Context(), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	instance, err := component.Instantiate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = instance.Close(t.Context()) }()
	f := instance.ExportedFunc("f")
	for _, argument := range []string{"x", ""} {
		if _, err := f.Call(t.Context(), wacogo.ValString(argument)); err != nil {
			t.Fatalf("string %q: %v", argument, err)
		}
	}
}
