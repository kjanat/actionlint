package invocation_test

import (
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
)

func TestResolutionOutcomesRemainDistinctWithoutScripts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		kind   invocation.OutcomeKind
		reason invocation.Reason
		make   func() (invocation.Outcome, error)
	}{
		{"dockerfile", invocation.OutcomeOpaque, invocation.ReasonDockerfileOnly, func() (invocation.Outcome, error) {
			return invocation.Opaque(invocation.ReasonDockerfileOnly, "Dockerfile")
		}},
		{"non-composite", invocation.OutcomeOpaque, invocation.ReasonNonComposite, func() (invocation.Outcome, error) { return invocation.Opaque(invocation.ReasonNonComposite, "node24") }},
		{"external", invocation.OutcomeUnavailable, invocation.ReasonNonLocal, func() (invocation.Outcome, error) {
			return invocation.Unavailable(invocation.ReasonNonLocal, "owner/action@v1")
		}},
		{"disabled", invocation.OutcomeUnavailable, invocation.ReasonLookupDisabled, func() (invocation.Outcome, error) {
			return invocation.Unavailable(invocation.ReasonLookupDisabled, "no repository")
		}},
		{"uncertain", invocation.OutcomeUnavailable, invocation.ReasonPlacementUncertain, func() (invocation.Outcome, error) {
			return invocation.Unavailable(invocation.ReasonPlacementUncertain, "conditional checkout")
		}},
		{"missing", invocation.OutcomeUnresolved, invocation.ReasonMissingManifest, func() (invocation.Outcome, error) {
			return invocation.Unresolved(invocation.ReasonMissingManifest, "missing manifest")
		}},
		{"parse", invocation.OutcomeUnresolved, invocation.ReasonParseFailed, func() (invocation.Outcome, error) {
			return invocation.Unresolved(invocation.ReasonParseFailed, "line 3: invalid YAML")
		}},
		{"mapping", invocation.OutcomeUnresolved, invocation.ReasonMappingFailed, func() (invocation.Outcome, error) {
			return invocation.Unresolved(invocation.ReasonMappingFailed, "ambiguous mapping")
		}},
		{"dynamic", invocation.OutcomeDynamic, invocation.ReasonExpression, func() (invocation.Outcome, error) { return invocation.Dynamic("${{ inputs.action }}") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := builder(t, invocation.EntryWorkflow, invocation.DefaultBudget())
			call := begin(t, b, b.RootID(), reference("entry", "call"))
			outcome, err := tc.make()
			if err != nil {
				t.Fatal(err)
			}
			callContext, admitted := call.Context()
			if !admitted {
				t.Fatal("call ticket was not admitted")
			}
			if _, err := b.Resolve(callContext, outcome); err != nil {
				t.Fatal(err)
			}
			g := publish(t, b)
			stored, ok := g.Call(call.ID())
			if !ok || stored.Outcome.Kind() != tc.kind || stored.Outcome.Reason() != tc.reason || stored.Outcome.Detail() != outcome.Detail() {
				t.Fatalf("lost typed resolution: %+v", stored)
			}
			if _, hasTarget := stored.Outcome.Target(); hasTarget {
				t.Fatal("non-expanded outcome fabricated a target")
			}
			if g.Incomplete() != (tc.kind != invocation.OutcomeOpaque) {
				t.Fatal("resolution completeness conflated opaque and unresolved bodies")
			}
		})
	}
}

func TestCycleBetweenAncestorsAndRepeatedSibling(t *testing.T) {
	b := builder(t, invocation.EntryWorkflow, invocation.DefaultBudget())
	first := begin(t, b, b.RootID(), reference("entry", "first"))
	expand(t, b, first, reference("outer", "document"))
	middle := begin(t, b, first.ID(), reference("outer", "middle"))
	expand(t, b, middle, reference("middle", "document"))
	closing := begin(t, b, middle.ID(), reference("middle", "cycle"))
	target, err := invocation.Expanded(reference("outer", "document"))
	if err != nil {
		t.Fatal(err)
	}
	closingContext, admitted := closing.Context()
	if !admitted {
		t.Fatal("cycle ticket was not admitted")
	}
	result, err := b.Resolve(closingContext, target)
	cycle, ok := result.Cycle()
	if err != nil || !ok || cycle.Owner != first.ID() || cycle.Closing != closing.ID() {
		t.Fatalf("wrong ancestor cycle: %+v, %v", cycle, err)
	}
	closeCall(t, b, middle.ID())
	closeCall(t, b, first.ID())
	sibling := begin(t, b, b.RootID(), reference("entry", "sibling"))
	expand(t, b, sibling, reference("outer", "document"))
	closeCall(t, b, sibling.ID())
	g := publish(t, b)
	repeated, _ := g.Call(sibling.ID())
	if repeated.Outcome.Kind() != invocation.OutcomeExpanded {
		t.Fatal("completed sibling treated as active ancestor")
	}
}

func TestInvalidOutcomeDoesNotConsumeReservation(t *testing.T) {
	b := builder(t, invocation.EntryWorkflow, invocation.DefaultBudget())
	call := begin(t, b, b.RootID(), reference("entry", "call"))
	if _, err := invocation.Unresolved(invocation.ReasonDockerfileOnly, "wrong category"); !errors.Is(err, invocation.ErrInvalidOutcome) {
		t.Fatalf("invalid category: %v", err)
	}
	if _, err := invocation.Dynamic("\xff"); !errors.Is(err, invocation.ErrInvalidOutcome) {
		t.Fatalf("invalid detail: %v", err)
	}
	if _, err := b.Interrupt(invocation.ReasonCycle, "wrong interruption"); !errors.Is(err, invocation.ErrInvalidOutcome) {
		t.Fatalf("invalid interruption: %v", err)
	}
	opaque(t, b, call)
	g := publish(t, b)
	if g.Incomplete() {
		t.Fatal("invalid operation changed completed graph")
	}
}
