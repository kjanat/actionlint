package component_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/partite-ai/wacogo"
)

// The canonical ABI checks only alignment and bounds on a realloc result, so a
// null pointer is valid for a zero-size request. TinyGo's cabi_realloc returns
// null there. wacogo traps instead. This test pins that runtime behaviour; a
// failure here means the runtime accepts it and TinyGo guests can be supported.
func TestRuntimeRejectsNullReallocForZeroSize(t *testing.T) {
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
	if _, err := f.Call(t.Context(), wacogo.ValString("x")); err != nil {
		t.Fatalf("non-empty string: %v", err)
	}
	_, err = f.Call(t.Context(), wacogo.ValString(""))
	if err == nil || !strings.Contains(err.Error(), "realloc returned null") {
		t.Fatalf("empty string: err=%v", err)
	}
}
