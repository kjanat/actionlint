package invocation_test

import (
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
)

func TestInterruptPreservesOwnedContext(t *testing.T) {
	for _, state := range []string{"entry", "pending", "expanded"} {
		t.Run(state, func(t *testing.T) {
			b := builder(t, invocation.EntryWorkflow, invocation.DefaultBudget())
			want := b.RootContext()
			target := reference("child", "document")
			if state != "entry" {
				ticket := begin(t, b, b.RootID(), reference("entry", "call"))
				var admitted bool
				want, admitted = ticket.Context()
				if !admitted {
					t.Fatal("call was not admitted")
				}
				if state == "expanded" {
					expand(t, b, ticket, target)
				}
			}
			got, err := b.Interrupt(invocation.ReasonCanceled, "cancelled")
			if err != nil || got != want || !b.Owns(got) {
				t.Fatalf("interruption lost context: got=%v want=%v error=%v", got.ID(), want.ID(), err)
			}
			if state != "entry" {
				if err := b.Close(got); err != nil {
					t.Fatalf("interrupted close: %v", err)
				}
			}
			if _, err := b.Begin(b.RootContext(), reference("entry", "later"), invocation.Facts{Condition: invocation.ConditionEnabled}); !errors.Is(err, invocation.ErrInterrupted) {
				t.Fatalf("post-interruption admission: %v", err)
			}
			graph := publish(t, b)
			stored, exists := graph.Context(got.ID())
			if !exists || stored != want || !graph.Owns(got) {
				t.Fatal("publication lost interrupted handle")
			}
			if state == "entry" {
				if _, stopped := graph.EntryInterruption(); !stopped {
					t.Fatal("entry interruption absent")
				}
				return
			}
			call, _ := graph.Call(got.ID())
			body, hasBody := call.Outcome.Target()
			if call.Outcome.Kind() != invocation.OutcomeInterrupted || hasBody != (state == "expanded") || hasBody && body != target {
				t.Fatalf("interruption changed body: %+v", call.Outcome)
			}
		})
	}
}

func TestContextsCannotCrossGraphsWithIdenticalIDs(t *testing.T) {
	first := builder(t, invocation.EntryWorkflow, invocation.DefaultBudget())
	second := builder(t, invocation.EntryWorkflow, invocation.DefaultBudget())
	if first.RootID() != second.RootID() || first.Owns(second.RootContext()) || first.Owns(invocation.Context{}) {
		t.Fatal("entry handle escaped its owner")
	}
	facts := invocation.Facts{Uses: "./child", HasUses: true, Condition: invocation.ConditionEnabled, Step: invocation.Position{Line: 3, Column: 7}, Background: true}
	site := reference("entry", "call")
	if _, err := first.Begin(second.RootContext(), site, facts); !errors.Is(err, invocation.ErrForeignContext) {
		t.Fatalf("foreign parent: %v", err)
	}
	if _, err := first.AddScript(second.RootContext(), reference("entry", "script"), invocation.ScriptRun, ""); !errors.Is(err, invocation.ErrForeignContext) {
		t.Fatalf("foreign script owner: %v", err)
	}
	left, err := first.Begin(first.RootContext(), site, facts)
	if err != nil {
		t.Fatal(err)
	}
	right, err := second.Begin(second.RootContext(), site, facts)
	if err != nil {
		t.Fatal(err)
	}
	leftContext, _ := left.Context()
	rightContext, _ := right.Context()
	if left.ID() != right.ID() || leftContext == rightContext || first.Owns(rightContext) {
		t.Fatal("equal stable IDs collapsed owner handles")
	}
	outcome, err := invocation.Opaque(invocation.ReasonNonComposite, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Resolve(rightContext, outcome); !errors.Is(err, invocation.ErrForeignContext) {
		t.Fatalf("foreign resolution: %v", err)
	}
	if err := first.Close(rightContext); !errors.Is(err, invocation.ErrForeignContext) {
		t.Fatalf("foreign close: %v", err)
	}
	if _, err := first.Resolve(leftContext, outcome); err != nil {
		t.Fatal(err)
	}
	graph := publish(t, first)
	stored, exists := graph.Context(left.ID())
	if !exists || stored != leftContext || !graph.Owns(leftContext) || graph.Owns(rightContext) || graph.Owns(invocation.Context{}) {
		t.Fatal("publication changed context ownership")
	}
	facts.Background = false
	projected, ok := stored.Facts()
	if !ok || !projected.Background {
		t.Fatal("caller mutation reached canonical facts")
	}
	projected.Background = false
	retained, _ := stored.Facts()
	if !retained.Background {
		t.Fatal("returned facts alias retained facts")
	}
	if _, ok := graph.RootContext().Facts(); ok {
		t.Fatal("entry fabricated call facts")
	}
}

func TestUnadmittedTicketsDoNotSupplyContexts(t *testing.T) {
	depth := builder(t, invocation.EntryWorkflow, invocation.Budget{MaxDepth: 1, MaxCalls: 5, MaxScripts: 5})
	first := begin(t, depth, depth.RootID(), reference("entry", "first"))
	expand(t, depth, first, reference("child", "document"))
	limited := begin(t, depth, first.ID(), reference("child", "nested"))
	if context, ok := limited.Context(); limited.Status() != invocation.DepthLimited || ok || context.Valid() {
		t.Fatal("depth frontier supplied an executable context")
	}
	closeCall(t, depth, first.ID())
	graph := publish(t, depth)
	if context, exists := graph.Context(limited.ID()); !exists || !context.Valid() {
		t.Fatal("depth frontier lost recorded call facts")
	}
	for _, scriptLimit := range []bool{false, true} {
		b := builder(t, invocation.EntryWorkflow, invocation.Budget{MaxDepth: 5, MaxCalls: 1, MaxScripts: 1})
		if scriptLimit {
			if _, err := b.AddScript(b.RootContext(), reference("entry", "first"), invocation.ScriptRun, ""); err != nil {
				t.Fatal(err)
			}
			if _, err := b.AddScript(b.RootContext(), reference("entry", "second"), invocation.ScriptRun, ""); !errors.Is(err, invocation.ErrScriptBudget) {
				t.Fatal(err)
			}
		} else {
			opaque(t, b, begin(t, b, b.RootID(), reference("entry", "first")))
		}
		stopped := begin(t, b, b.RootID(), reference("entry", "later"))
		if context, ok := stopped.Context(); stopped.Status() != invocation.Stopped || ok || context.Valid() || stopped.ID() != "" {
			t.Fatal("stopped admission supplied a context")
		}
		if _, err := b.Begin(b.RootContext(), reference("entry", "invalid"), invocation.Facts{}); !errors.Is(err, invocation.ErrInvalidFacts) {
			t.Fatalf("stop hid invalid facts: %v", err)
		}
	}
	var zero invocation.Ticket
	if context, ok := zero.Context(); ok || context.Valid() {
		t.Fatal("zero ticket supplied a context")
	}
}

func TestCallSiteMustBelongToParentRevision(t *testing.T) {
	for _, field := range []string{"document", "revision"} {
		t.Run(field, func(t *testing.T) {
			b := builder(t, invocation.EntryWorkflow, invocation.DefaultBudget())
			site := reference("entry", "call")
			if field == "document" {
				site.Document = "foreign"
			} else {
				site.Revision = "stale"
			}
			if _, err := b.Begin(b.RootContext(), site, invocation.Facts{Condition: invocation.ConditionEnabled}); !errors.Is(err, invocation.ErrInvalidSource) {
				t.Fatalf("foreign source accepted: %v", err)
			}
		})
	}
}

func TestFactsRejectContradictoryAndMalformedValues(t *testing.T) {
	for name, facts := range map[string]invocation.Facts{
		"invalid condition":    {},
		"absent nonempty uses": {Uses: "./child", Condition: invocation.ConditionEnabled},
		"invalid UTF8":         {Uses: "\xff", HasUses: true, Condition: invocation.ConditionEnabled},
		"negative line":        {Step: invocation.Position{Line: -1, Column: 1}, Condition: invocation.ConditionEnabled},
		"partial position":     {Step: invocation.Position{Line: 1}, Condition: invocation.ConditionEnabled},
	} {
		t.Run(name, func(t *testing.T) {
			b := builder(t, invocation.EntryWorkflow, invocation.DefaultBudget())
			if _, err := b.Begin(b.RootContext(), reference("entry", "call"), facts); !errors.Is(err, invocation.ErrInvalidFacts) {
				t.Fatalf("invalid facts accepted: %v", err)
			}
		})
	}
}
