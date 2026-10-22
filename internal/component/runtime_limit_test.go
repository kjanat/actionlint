package component_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/internal/component"
	"github.com/partite-ai/wacogo"
)

func TestLoadBoundsCanonicalDescriptorAllocations(t *testing.T) {
	for _, name := range []string{"oversized-descriptor-values", "oversized-descriptor-bytes"} {
		t.Run(name, func(t *testing.T) {
			plugin, err := component.Load(t.Context(), filepath.Join("testdata", "wat", name+".wasm"), component.Options{})
			if err == nil {
				_ = plugin.Close(context.Background())
				t.Fatal("loaded a descriptor exceeding host allocation limits")
			}
			if !errors.Is(err, wacogo.ErrLiftLimit) {
				t.Fatalf("expected canonical lifting limit, got %v", err)
			}
		})
	}
}

// The canonical ABI checks only alignment and bounds on a realloc result, so a
// null pointer is valid for a zero-size request, which TinyGo's cabi_realloc
// returns. wacogo accepts it since partite-ai/wacogo#12 (partite-ai/wacogo#11).
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
