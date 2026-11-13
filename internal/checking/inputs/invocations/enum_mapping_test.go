package invocations

import (
	"errors"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
)

func TestProjectionRejectsUnknownEnumMappings(t *testing.T) {
	t.Parallel()
	_, entry := projectEntryKind("unknown")
	_, script := projectScriptKind("unknown")
	_, opaque := projectOpaqueReason("unknown")
	_, unavailable := projectUnavailableReason("unknown")
	_, unresolved := projectUnresolvedReason("unknown")
	_, interrupted := projectInterruptionReason("unknown")
	for name, err := range map[string]error{"entry": entry, "script": script, "opaque": opaque, "unavailable": unavailable, "unresolved": unresolved, "interrupted": interrupted} {
		if !errors.Is(err, checkinputs.ErrInvalidCallGraph) {
			t.Errorf("%s accepted unknown enum: %v", name, err)
		}
	}
}

func TestProjectionEntryOperationalInterruption(t *testing.T) {
	t.Parallel()
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
	graph := projection.Graph()
	interruption, exists := graph.EntryInterruption().Get()
	if !exists || interruption.Reason != checkinputs.OperationalFailure || interruption.Detail != "unavailable" || graph.Complete() {
		t.Fatalf("operational interruption lost: %+v", graph.Export())
	}
}
