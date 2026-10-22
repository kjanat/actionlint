package component

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifact"
	"github.com/kjanat/wacogo"
	"golang.org/x/sync/semaphore"
)

func TestRejectsMemoryPagesAboveWasmMaximum(t *testing.T) {
	if _, err := Load(t.Context(), "not-opened.wasm", Options{MemoryPages: 65537}); err == nil || !strings.Contains(err.Error(), "65536 pages") {
		t.Fatalf("invalid memory limit was not rejected: %v", err)
	}
}

func TestLoadArtifactRejectsInvalidMemoryLimit(t *testing.T) {
	artifact := &pluginartifact.Artifact{Info: pluginartifact.Info{Source: "in-memory"}}
	if _, err := LoadArtifact(t.Context(), artifact, Options{MemoryPages: 65537}); err == nil || !strings.Contains(err.Error(), "65536 pages") {
		t.Fatalf("in-memory artifact bypassed memory option validation: %v", err)
	}
}

func TestComponentCoordinatesAreOneBasedAndRepresentable(t *testing.T) {
	for _, coordinate := range []struct{ line, column uint32 }{{0, 1}, {1, 0}, {1, 1}, {math.MaxUint32, 1}, {1, math.MaxUint32}} {
		value := wacogo.NewValRecord(field("line", wacogo.ValU32(coordinate.line)), field("column", wacogo.ValU32(coordinate.column)))
		position, err := liftPosition(value)
		invalid := coordinate.line == 0 || coordinate.column == 0 || strconv.IntSize == 32 && (coordinate.line > math.MaxInt32 || coordinate.column > math.MaxInt32)
		if invalid {
			if err == nil {
				t.Fatalf("invalid coordinates accepted: %v", position)
			}
		} else if err != nil || uint64(position.Line) != uint64(coordinate.line) || uint64(position.Column) != uint64(coordinate.column) {
			t.Fatalf("valid coordinates changed: %+v err=%v", coordinate, err)
		}
	}
}

func TestQueuedPluginCallsHonorCallerCancellationAndDeadline(t *testing.T) {
	for _, callerCancels := range []bool{false, true} {
		p := &Plugin{limits: Limits{Timeout: time.Second}, gate: semaphore.NewWeighted(1)}
		if err := p.gate.Acquire(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		finished := make(chan error, 1)
		go func() { _, err := p.call(ctx, "analyze"); finished <- err }()
		if callerCancels {
			cancel()
		}
		select {
		case err := <-finished:
			if callerCancels {
				if !errors.Is(err, context.Canceled) {
					t.Errorf("queued call error=%v, want cancellation", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "exceeded its time limit") {
				t.Errorf("queued call error=%v, want deadline failure", err)
			}
		case <-time.After(time.Second):
			t.Error("queued call ignored caller cancellation or deadline")
		}
		cancel()
		p.gate.Release(1)
	}
}

func TestPluginExecutionBudgetStartsAfterQueue(t *testing.T) {
	p := &Plugin{limits: Limits{Timeout: 5 * time.Millisecond}, gate: semaphore.NewWeighted(1)}
	if err := p.gate.Acquire(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { _, err := p.call(t.Context(), "analyze"); finished <- err }()
	select {
	case err := <-finished:
		p.gate.Release(1)
		t.Fatalf("execution budget elapsed in queue: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	p.gate.Release(1)
	if err := <-finished; err == nil || !strings.Contains(err.Error(), "is closed") {
		t.Fatalf("call did not reach execution after queue: %v", err)
	}
}

func TestQueuedCloseHonorsCancellation(t *testing.T) {
	p := &Plugin{gate: semaphore.NewWeighted(1)}
	if err := p.gate.Acquire(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	defer p.gate.Release(1)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := p.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close ignored cancellation: %v", err)
	}
}
