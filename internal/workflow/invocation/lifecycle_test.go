package invocation_test

import (
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	"actionlint.kjanat.dev/v2/source"
)

func reference(document, node string) source.Reference {
	return source.Reference{Document: document, Node: node, Revision: "snapshot-1"}
}

func builder(t *testing.T, kind invocation.EntryKind, budget invocation.Budget) *invocation.Builder {
	t.Helper()
	b, err := invocation.NewBuilder(kind, reference("entry", "document"), budget)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func begin(t *testing.T, b *invocation.Builder, parent invocation.ID, site source.Reference) invocation.Ticket {
	t.Helper()
	ticket, err := b.Begin(parent, site, "./action", invocation.ConditionEnabled)
	if err != nil {
		t.Fatal(err)
	}
	return ticket
}

func expand(t *testing.T, b *invocation.Builder, ticket invocation.Ticket, target source.Reference) {
	t.Helper()
	outcome, err := invocation.Expanded(target)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := b.Resolve(ticket.ID(), outcome)
	if err != nil || resolved.Kind() != invocation.OutcomeExpanded {
		t.Fatalf("expand: %v, %v", resolved, err)
	}
}

func TestInterruptionStopsAdmission(t *testing.T) {
	for _, atEntry := range []bool{true, false} {
		name := "call"
		if atEntry {
			name = "entry"
		}
		t.Run(name, func(t *testing.T) {
			b := builder(t, invocation.EntryWorkflow, invocation.DefaultBudget())
			if atEntry {
				if err := b.InterruptEntry(invocation.ReasonCanceled, "canceled"); err != nil {
					t.Fatal(err)
				}
			} else {
				begin(t, b, b.RootID(), reference("entry", "first"))
				if _, err := b.Interrupt(invocation.ReasonCanceled, "canceled"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := b.Begin(b.RootID(), reference("entry", "after-cancel"), "./later", invocation.ConditionEnabled); err == nil {
				t.Fatal("admitted a call after interruption")
			}
			g, err := b.Publish()
			if err != nil || !g.Incomplete() {
				t.Fatalf("interrupted graph: incomplete=%v, err=%v", g.Incomplete(), err)
			}
		})
	}
}

func TestTraversalRejectsUnfinishedSibling(t *testing.T) {
	for _, expanded := range []bool{false, true} {
		name := "pending"
		if expanded {
			name = "expanded"
		}
		t.Run(name, func(t *testing.T) {
			b := builder(t, invocation.EntryWorkflow, invocation.DefaultBudget())
			first := begin(t, b, b.RootID(), reference("entry", "first"))
			if expanded {
				expand(t, b, first, reference("action", "document"))
			}
			if _, err := b.Begin(b.RootID(), reference("entry", "sibling"), "./second", invocation.ConditionEnabled); err == nil {
				t.Fatal("admitted sibling before finishing current call")
			}
		})
	}
}

func TestInterruptionBetweenTopLevelCalls(t *testing.T) {
	for _, exhausted := range []bool{false, true} {
		name := "completed-call"
		if exhausted {
			name = "exhausted-budget"
		}
		t.Run(name, func(t *testing.T) {
			b := builder(t, invocation.EntryWorkflow, invocation.Budget{MaxDepth: 10, MaxCalls: 1})
			first := begin(t, b, b.RootID(), reference("entry", "first"))
			opaque(t, b, first)
			if exhausted {
				stopped := begin(t, b, b.RootID(), reference("entry", "second"))
				if stopped.Status() != invocation.Stopped {
					t.Fatal("budget was not exhausted")
				}
			}
			id, err := b.Interrupt(invocation.ReasonCanceled, "canceled between calls")
			if err != nil || id != b.RootID() {
				t.Fatalf("entry-level interruption: %s, %v", id, err)
			}
			g := publish(t, b)
			interruption, exists := g.EntryInterruption()
			if !exists || !g.Incomplete() || interruption.Reason() != invocation.ReasonCanceled || len(g.Calls()) != 1 {
				t.Fatalf("lost entry interruption or completed calls: %+v", g)
			}
			completed, _ := g.Call(first.ID())
			if completed.Outcome.Kind() != invocation.OutcomeOpaque {
				t.Fatal("interruption rewrote a completed call")
			}
		})
	}
}

func TestUnwindingDoesNotDuplicateInterruption(t *testing.T) {
	b := builder(t, invocation.EntryWorkflow, invocation.DefaultBudget())
	outer := begin(t, b, b.RootID(), reference("entry", "outer"))
	expand(t, b, outer, reference("outer", "document"))
	inner := begin(t, b, outer.ID(), reference("outer", "inner"))
	if _, err := b.Interrupt(invocation.ReasonCanceled, "canceled"); err != nil {
		t.Fatal(err)
	}
	closeCall(t, b, inner.ID())
	closeCall(t, b, outer.ID())
	if err := b.InterruptEntry(invocation.ReasonCanceled, "duplicate"); !errors.Is(err, invocation.ErrAlreadyInterrupted) {
		t.Fatalf("recorded a second interruption: %v", err)
	}
	g := publish(t, b)
	if _, exists := g.EntryInterruption(); exists {
		t.Fatal("call interruption was duplicated on entry")
	}
}
