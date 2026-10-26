package invocationview

import (
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
)

func TestProjectionRejectsUnknownEnumMappings(t *testing.T) {
	_, entry := projectEntryKind("unknown")
	_, script := projectScriptKind("unknown")
	_, opaque := projectOpaqueReason("unknown")
	_, unavailable := projectUnavailableReason("unknown")
	_, unresolved := projectUnresolvedReason("unknown")
	_, interrupted := projectInterruptionReason("unknown")
	for name, err := range map[string]error{"entry": entry, "script": script, "opaque": opaque, "unavailable": unavailable, "unresolved": unresolved, "interrupted": interrupted} {
		if !errors.Is(err, analyzer.ErrInvalidCallGraph) {
			t.Errorf("%s accepted unknown enum: %v", name, err)
		}
	}
}

func TestProjectionEntryOperationalInterruption(t *testing.T) {
	builder, err := invocation.NewBuilder(invocation.EntryWorkflow, ref("workflow", "document"), invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.InterruptEntry(invocation.ReasonOperational, "unavailable"); err != nil {
		t.Fatal(err)
	}
	projection, err := New(publish(t, builder))
	if err != nil {
		t.Fatal(err)
	}
	graph := projection.Snapshot()
	interruption, exists := graph.EntryInterruption.Get()
	if !exists || interruption.Reason != analyzer.OperationalFailure || interruption.Detail != "unavailable" || graph.Complete {
		t.Fatalf("operational interruption lost: %+v", graph)
	}
}
