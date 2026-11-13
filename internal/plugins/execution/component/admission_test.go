package component_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/component"
)

func TestQueuedCallHonorsCancellationAndTimeout(t *testing.T) {
	t.Parallel()
	for _, cancelQueued := range []bool{true, false} {
		t.Run(map[bool]string{true: "cancellation", false: "timeout"}[cancelQueued], func(t *testing.T) {
			t.Parallel()
			entered, release := make(chan struct{}), make(chan struct{})
			plugin := loadPlugin(t, "misbehave", component.Options{
				Timeout: 2 * time.Second,
				Log: func(line string) {
					if strings.HasPrefix(line, "analyzing ") {
						close(entered)
						<-release
					}
				},
			})
			first := make(chan error, 1)
			go func() {
				_, err := plugin.CheckDocument(t.Context(), checkinputs.Input{Options: json.RawMessage("log")})
				first <- err
			}()
			defer func() {
				close(release)
				if err := <-first; err != nil && !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "exceeded its time limit") {
					t.Errorf("active invocation: %v", err)
				}
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("first call did not enter guest")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			second := make(chan error, 1)
			go func() {
				_, err := plugin.CheckDocument(ctx, checkinputs.Input{Options: json.RawMessage("ok")})
				second <- err
			}()
			if cancelQueued {
				cancel()
			}
			select {
			case err := <-second:
				if cancelQueued && !errors.Is(err, context.Canceled) {
					t.Fatalf("queued cancellation: %v", err)
				}
				if !cancelQueued && (err == nil || !strings.Contains(err.Error(), "exceeded its time limit")) {
					t.Fatalf("queued timeout: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("queued call waits for active invocation")
			}
		})
	}
}
