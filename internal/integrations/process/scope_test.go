package process

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestScopeJoinsAllResourcesOnce(t *testing.T) {
	t.Parallel()
	failure := errors.New("resource failed")
	var calls atomic.Int32
	var scope Scope
	scope.Register(func() error { calls.Add(1); return failure })
	scope.Register(func() error { calls.Add(1); return nil })
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			if err := scope.Finish(); !errors.Is(err, failure) {
				t.Errorf("Finish: %v", err)
			}
		})
	}
	group.Wait()
	if calls.Load() != 2 {
		t.Fatalf("joined %d resources", calls.Load())
	}
}
