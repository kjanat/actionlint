package results

import (
	"errors"
	"fmt"
	"testing"
)

func TestConsolidateCountsWrappedBranchesWithoutRecountingAggregates(t *testing.T) {
	t.Parallel()
	failure := errors.New("source unavailable")
	first := ConsolidateRejectedRelated(failure, 2)
	aggregate := ConsolidateRejectedRelated(fmt.Errorf("bridge: %w", first), 3)
	prior := errors.Join(fmt.Errorf("adapter: %w", aggregate), NewRejectedRelatedError(7))
	err := ConsolidateRejectedRelated(prior, 11)
	rejected, ok := errors.AsType[*RejectedRelatedError](err)
	if !ok || rejected.Count() != 23 || !errors.Is(err, failure) || !errors.Is(err, prior) {
		t.Fatalf("consolidation lost branch counts or causes: %v", err)
	}
	if got := ConsolidateRejectedRelated(failure, 0); !errors.Is(got, failure) {
		t.Fatalf("empty consolidation replaced cause: %v", got)
	}
}
