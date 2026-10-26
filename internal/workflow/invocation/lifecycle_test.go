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

func contextFor(t *testing.T, b *invocation.Builder, id invocation.ID) invocation.Context {
	t.Helper()
	if id == b.RootID() {
		return b.RootContext()
	}
	ctx, ok := b.Context(id)
	if !ok {
		t.Fatalf("missing context for %q", id)
	}
	return ctx
}

func begin(t *testing.T, b *invocation.Builder, parent invocation.ID, site source.Reference) invocation.Ticket {
	t.Helper()
	ticket, err := b.Begin(contextFor(t, b, parent), site, invocation.Facts{Uses: "./action", HasUses: true, Condition: invocation.ConditionEnabled})
	if err != nil {
		t.Fatal(err)
	}
	return ticket
}

func addScript(t *testing.T, b *invocation.Builder, owner invocation.ID, declaration source.Reference, kind invocation.ScriptKind, input string) (invocation.ID, error) {
	t.Helper()
	return b.AddScript(contextFor(t, b, owner), declaration, kind, input)
}

func expand(t *testing.T, b *invocation.Builder, ticket invocation.Ticket, target source.Reference) {
	t.Helper()
	outcome, err := invocation.Expanded(target)
	if err != nil {
		t.Fatal(err)
	}
	ctx, admitted := ticket.Context()
	if !admitted {
		t.Fatal("expanded ticket was not admitted")
	}
	resolved, err := b.Resolve(ctx, outcome)
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
			if _, err := b.Begin(b.RootContext(), reference("entry", "after-cancel"), invocation.Facts{Uses: "./later", HasUses: true, Condition: invocation.ConditionEnabled}); err == nil {
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
			if _, err := b.Begin(b.RootContext(), reference("entry", "sibling"), invocation.Facts{Uses: "./second", HasUses: true, Condition: invocation.ConditionEnabled}); err == nil {
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
			b := builder(t, invocation.EntryWorkflow, invocation.Budget{MaxDepth: 10, MaxCalls: 1, MaxScripts: invocation.DefaultMaxScripts})
			first := begin(t, b, b.RootID(), reference("entry", "first"))
			opaque(t, b, first)
			if exhausted {
				stopped := begin(t, b, b.RootID(), reference("entry", "second"))
				if stopped.Status() != invocation.Stopped {
					t.Fatal("budget was not exhausted")
				}
			}
			ctx, err := b.Interrupt(invocation.ReasonCanceled, "canceled between calls")
			if err != nil || ctx.ID() != b.RootID() {
				t.Fatalf("entry-level interruption: %s, %v", ctx.ID(), err)
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
