package component

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestCallCloseDoesNotRunGuestDestructors(t *testing.T) {
	for _, operation := range []string{"trap", "hang"} {
		t.Run(operation, func(t *testing.T) {
			plugin, err := Load(t.Context(), filepath.Join("testdata", "wat", "close-failure.wasm"), Options{Timeout: 2 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = plugin.Close(context.Background()) })
			baseline := plugin.memory.used
			for range 2 {
				value, err := plugin.call(t.Context(), operation)
				if err != nil {
					t.Fatalf("closing the guest invoked its %s destructor: %v", operation, err)
				}
				descriptor, err := liftDescriptor(value)
				if err != nil || descriptor.Name != "close-fixture" {
					t.Fatalf("call result: descriptor=%+v error=%v", descriptor, err)
				}
				if plugin.engine == nil || plugin.memory.used != baseline {
					t.Fatalf("runtime=%v memory=%d; want reusable runtime and memory=%d", plugin.engine != nil, plugin.memory.used, baseline)
				}
			}
			if _, err := plugin.describe(t.Context()); err != nil {
				t.Fatalf("reuse after guest close: %v", err)
			}
			if err := plugin.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			if plugin.memory.used != 0 {
				t.Fatalf("plugin close left %d bytes accounted", plugin.memory.used)
			}
		})
	}
}
