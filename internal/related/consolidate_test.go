package related

import (
	"errors"
	"fmt"
	"testing"
)

func TestConsolidateCountsWrappedBranchesWithoutRecountingAggregates(t *testing.T) {
	failure := errors.New("source unavailable")
	first := Consolidate(failure, 2)
	aggregate := Consolidate(fmt.Errorf("bridge: %w", first), 3)
	prior := errors.Join(fmt.Errorf("adapter: %w", aggregate), &RejectedError{Withheld: 7})
	err := Consolidate(prior, 11)
	rejected, ok := errors.AsType[*RejectedError](err)
	if !ok || rejected.Count() != 23 || !errors.Is(err, failure) || !errors.Is(err, prior) {
		t.Fatalf("consolidation lost branch counts or causes: %v", err)
	}
	if got := Consolidate(failure, 0); !errors.Is(got, failure) {
		t.Fatalf("empty consolidation replaced cause: %v", got)
	}
}
