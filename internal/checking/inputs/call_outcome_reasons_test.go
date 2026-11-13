package inputs

import (
	"errors"
	"testing"
)

func TestCallOutcomeRejectsUnknownNonemptyReasons(t *testing.T) {
	t.Parallel()
	for name, value := range map[string]CallResolution{
		"opaque":      OpaqueCall{Reason: OpaqueReason("future-reason")},
		"unavailable": UnavailableCall{Reason: UnavailableReason("future-reason")},
		"unresolved":  UnresolvedCall{Reason: UnresolvedReason("future-reason")},
		"interrupted": InterruptedCall{Interruption: Interruption{Reason: InterruptionReason("future-reason")}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewCallOutcome(value); !errors.Is(err, ErrInvalidCallGraph) {
				t.Fatalf("unknown reason accepted: %v", err)
			}
		})
	}
}
