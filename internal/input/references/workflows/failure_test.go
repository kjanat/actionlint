package workflows

import (
	"context"
	"errors"
	"os"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/support/capturefixture"
	"actionlint.kjanat.dev/v2/internal/input/source/capture"
)

func TestCachedWorkflowFailuresPreserveCauses(t *testing.T) {
	t.Parallel()
	cause := &os.PathError{Op: "read", Path: "callee.yml", Err: context.Canceled}
	for _, failure := range []metadataError{readFailure("./callee.yml", cause), parseFailure("./callee.yml", cause)} {
		cache := NewCache(t.TempDir(), nil)
		cache.store("./callee.yml", reusableWorkflowMetadataResult{failure: failure})
		for _, spec := range []string{"./callee.yml", "$/callee.yml"} {
			metadata, err := cache.LoadForCall(t.Context(), spec)
			if metadata != nil || !errors.Is(err, context.Canceled) {
				t.Errorf("cached failure lost cancellation: metadata=%v error=%v", metadata, err)
			}
			if underlying, ok := errors.AsType[*os.PathError](err); !ok || underlying != cause {
				t.Errorf("cached failure lost its typed cause: %v", err)
			}
		}
	}
}

func TestWorkflowReadFailurePreservesCauseThroughCapture(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cause := &os.PathError{Op: "read", Path: "callee.yml", Err: context.Canceled}
	reads := 0
	store, err := capture.New(t.Context(), nil, func(context.Context, string) ([]byte, error) {
		reads++
		return nil, cause
	})
	if err != nil {
		t.Fatal(err)
	}
	capturefixture.CloseCaptures(t, store)
	cache := NewCache(root, nil)
	cache.SetSources(t.Context(), store, root)
	for _, spec := range []string{"./callee.yml", "$/callee.yml"} {
		metadata, err := cache.LoadForCall(t.Context(), spec)
		if metadata != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("captured read failure lost cancellation: metadata=%v error=%v", metadata, err)
		}
		if underlying, ok := errors.AsType[*os.PathError](err); !ok || underlying != cause {
			t.Fatalf("captured read failure lost its typed cause: %v", err)
		}
	}
	if reads != 1 {
		t.Fatalf("cached failure was read %d times, want one read", reads)
	}
}
