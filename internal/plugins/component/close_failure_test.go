package component

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCallReportsGuestCloseFailureAndDiscardsRuntime(t *testing.T) {
	for _, operation := range []string{"trap", "hang"} {
		t.Run(operation, func(t *testing.T) {
			plugin, err := Load(t.Context(), filepath.Join("testdata", "wat", "close-failure.wasm"), Options{Timeout: 2 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = plugin.Close(context.Background()) })
			started := time.Now()
			_, err = plugin.call(t.Context(), operation)
			if err == nil {
				t.Fatal("guest destructor failure was reported as success")
			}
			if operation == "trap" && !strings.Contains(err.Error(), "unreachable") {
				t.Fatalf("trap: %v", err)
			}
			if operation == "hang" && !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "time limit") {
				t.Fatalf("hang: %v", err)
			}
			if time.Since(started) > 5*time.Second {
				t.Fatalf("unbounded destructor: %v", err)
			}
			if plugin.engine != nil || plugin.memory.used != 0 {
				t.Fatalf("cleanup retained runtime=%v memory=%d", plugin.engine != nil, plugin.memory.used)
			}
			if _, err := plugin.call(t.Context(), "describe"); err == nil || !strings.Contains(err.Error(), "closed") {
				t.Fatalf("reused failed runtime: %v", err)
			}
		})
	}
}
