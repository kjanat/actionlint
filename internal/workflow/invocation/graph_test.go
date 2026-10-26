package invocation_test

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"testing"

	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	"actionlint.kjanat.dev/v2/source"
)

func closeCall(t *testing.T, b *invocation.Builder, id invocation.ID) {
	t.Helper()
	if err := b.Close(contextFor(t, b, id)); err != nil {
		t.Fatal(err)
	}
}

func publish(t *testing.T, b *invocation.Builder) invocation.Graph {
	t.Helper()
	g, err := b.Publish()
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func opaque(t *testing.T, b *invocation.Builder, ticket invocation.Ticket) {
	t.Helper()
	o, err := invocation.Opaque(invocation.ReasonNonComposite, "JavaScript action")
	if err != nil {
		t.Fatal(err)
	}
	ctx, admitted := ticket.Context()
	if !admitted {
		t.Fatal("opaque ticket was not admitted")
	}
	if _, err := b.Resolve(ctx, o); err != nil {
		t.Fatal(err)
	}
}

func TestRepeatedCallsRetainExactAncestry(t *testing.T) {
	b := builder(t, invocation.EntryWorkflow, invocation.DefaultBudget())
	var branches [][]invocation.ID
	var sites [][]source.Reference
	var scripts []invocation.ID
	for _, branch := range []string{"left", "right"} {
		parent := b.RootID()
		chain := []invocation.ID{parent}
		callSites := []source.Reference{
			reference("entry", branch),
			reference("outer", "middle-call"),
			reference("middle", "inner-call"),
		}
		for i, target := range []string{"outer", "middle", "inner"} {
			ticket := begin(t, b, parent, callSites[i])
			expand(t, b, ticket, reference(target, "document"))
			parent = ticket.ID()
			chain = append(chain, parent)
		}
		script, err := b.AddScript(contextFor(t, b, parent), reference("inner", "run"), invocation.ScriptRun, "")
		if err != nil {
			t.Fatal(err)
		}
		scripts = append(scripts, script)
		for i := len(chain) - 1; i > 0; i-- {
			closeCall(t, b, chain[i])
		}
		branches = append(branches, chain)
		sites = append(sites, callSites)
	}
	g := publish(t, b)
	if g.Incomplete() || len(g.Calls()) != 6 {
		t.Fatalf("complete repeated graph: incomplete=%v calls=%d", g.Incomplete(), len(g.Calls()))
	}
	for branch, chain := range branches {
		got, ok := g.Chain(chain[3])
		if !ok || !reflect.DeepEqual(got, chain) {
			t.Fatalf("branch %d chain %v, want %v", branch, got, chain)
		}
		path, ok := g.Path(chain[3])
		if !ok || len(path) != 3 {
			t.Fatalf("branch %d path: %v", branch, path)
		}
		for i, call := range path {
			if call.Site() != sites[branch][i] || call.Parent() != chain[i] || call.Depth() != i+1 {
				t.Errorf("branch %d edge %d lost its source or parent: %+v", branch, i, call)
			}
		}
	}
	left, _ := g.Call(branches[0][3])
	right, _ := g.Call(branches[1][3])
	leftTarget, _ := left.Outcome.Target()
	rightTarget, _ := right.Outcome.Target()
	if left.ID() == right.ID() || left.Site() != right.Site() || leftTarget != rightTarget {
		t.Fatal("repeated declaration must retain source identity and have distinct occurrences")
	}
	leftScript, foundLeft := g.Script(scripts[0])
	rightScript, foundRight := g.Script(scripts[1])
	if !foundLeft || !foundRight || rightScript.ID == leftScript.ID || leftScript.Owner != left.ID() || rightScript.Owner != right.ID() || leftScript.Declaration != rightScript.Declaration {
		t.Fatalf("repeated scripts lost their distinct owners or shared declaration: %+v %+v", leftScript, rightScript)
	}
	mutated := g.Calls()
	mutatedContextSite := mutated[0].Context.Site()
	mutatedContextSite.Document = "corrupted"
	stored, _ := g.Call(branches[0][1])
	if stored.Site().Document != "entry" {
		t.Fatal("caller mutated graph snapshot")
	}
}

func TestStandaloneEntryDepthAndCycle(t *testing.T) {
	b := builder(t, invocation.EntryAction, invocation.DefaultBudget())
	first := begin(t, b, b.RootID(), reference("entry", "nested"))
	expand(t, b, first, reference("nested", "document"))
	closing := begin(t, b, first.ID(), reference("nested", "back-to-entry"))
	target, err := invocation.Expanded(reference("entry", "document"))
	if err != nil {
		t.Fatal(err)
	}
	closingContext, admitted := closing.Context()
	if !admitted {
		t.Fatal("cycle ticket was not admitted")
	}
	outcome, err := b.Resolve(closingContext, target)
	cycle, ok := outcome.Cycle()
	if err != nil || !ok || cycle.Owner != b.RootID() || cycle.Closing != closing.ID() {
		t.Fatalf("standalone cycle: %+v, %v", cycle, err)
	}
	closeCall(t, b, first.ID())
	g := publish(t, b)
	if depth, ok := g.Depth(g.RootID()); !ok || depth != 0 {
		t.Fatalf("standalone root depth %d", depth)
	}
	if depth, ok := g.Depth(first.ID()); !ok || depth != 1 {
		t.Fatalf("first nested call depth %d", depth)
	}
	if !g.Incomplete() {
		t.Fatal("cycle was reported as complete expansion")
	}
}

func TestDepthBudgetStopsBeforeLookup(t *testing.T) {
	for _, kind := range []invocation.EntryKind{invocation.EntryWorkflow, invocation.EntryAction} {
		for _, maxDepth := range []int{1, invocation.DefaultMaxDepth} {
			t.Run(fmt.Sprintf("%s/%d", kind, maxDepth), func(t *testing.T) {
				b := builder(t, kind, invocation.Budget{MaxDepth: maxDepth, MaxCalls: 100, MaxScripts: invocation.DefaultMaxScripts})
				parent := b.RootID()
				document := "entry"
				var expanded []invocation.ID
				for depth := 1; depth <= maxDepth; depth++ {
					ticket := begin(t, b, parent, reference(document, "call"))
					document = strconv.Itoa(depth)
					expand(t, b, ticket, reference(document, "document"))
					expanded = append(expanded, ticket.ID())
					parent = ticket.ID()
				}
				frontier := begin(t, b, parent, reference(document, "call"))
				if frontier.Status() != invocation.DepthLimited {
					t.Fatal("depth frontier requested metadata lookup")
				}
				frontierContext := contextFor(t, b, frontier.ID())
				if _, err := b.Resolve(frontierContext, invocation.Outcome{}); !errors.Is(err, invocation.ErrNotPending) {
					t.Fatalf("frontier can be resolved: %v", err)
				}
				for _, e := range slices.Backward(expanded) {
					closeCall(t, b, e)
				}
				g := publish(t, b)
				call, ok := g.Call(frontier.ID())
				limit, maximum, limited := call.Outcome.Limit()
				if !ok || call.Depth() != maxDepth+1 || !limited || limit != invocation.LimitDepth || maximum != maxDepth || !g.Incomplete() {
					t.Fatalf("wrong depth frontier: %+v", call)
				}
			})
		}
	}
}

func TestTotalBudgetRetainsOneBoundary(t *testing.T) {
	b := builder(t, invocation.EntryWorkflow, invocation.Budget{MaxDepth: 10, MaxCalls: 2, MaxScripts: invocation.DefaultMaxScripts})
	for i := range 2 {
		opaque(t, b, begin(t, b, b.RootID(), reference("entry", strconv.Itoa(i))))
	}
	for i := 2; i < 102; i++ {
		ticket := begin(t, b, b.RootID(), reference("entry", strconv.Itoa(i)))
		if ticket.Status() != invocation.Stopped || ticket.ID() != "" {
			t.Fatal("total budget allocated a rejected call")
		}
	}
	if _, err := b.Begin(b.RootContext(), reference("entry", "0"), invocation.Facts{Uses: "./duplicate", HasUses: true, Condition: invocation.ConditionEnabled}); !errors.Is(err, invocation.ErrDuplicateCall) {
		t.Fatalf("budget hid duplicate traversal: %v", err)
	}
	g := publish(t, b)
	stop, ok := g.Stop()
	if len(g.Calls()) != 2 || !ok || stop.Parent != g.RootID() || stop.Site != reference("entry", "2") || stop.Max != 2 || stop.Limit != invocation.LimitTotal || !g.Incomplete() {
		t.Fatalf("unbounded or incorrect total frontier: %+v", stop)
	}
}

func TestInterruptionPreservesAncestorsAndCompletedCalls(t *testing.T) {
	for _, resolved := range []bool{false, true} {
		t.Run(fmt.Sprintf("resolved=%v", resolved), func(t *testing.T) {
			b := builder(t, invocation.EntryWorkflow, invocation.DefaultBudget())
			completed := begin(t, b, b.RootID(), reference("entry", "completed"))
			opaque(t, b, completed)
			outer := begin(t, b, b.RootID(), reference("entry", "outer"))
			expand(t, b, outer, reference("outer", "document"))
			inner := begin(t, b, outer.ID(), reference("outer", "inner"))
			if resolved {
				expand(t, b, inner, reference("inner", "document"))
			}
			ctx, err := b.Interrupt(invocation.ReasonCanceled, "context canceled")
			if err != nil || ctx.ID() != inner.ID() {
				t.Fatalf("interrupted %s: %v", ctx.ID(), err)
			}
			closeCall(t, b, inner.ID())
			closeCall(t, b, outer.ID())
			g := publish(t, b)
			parent, _ := g.Call(outer.ID())
			child, _ := g.Call(inner.ID())
			previous, _ := g.Call(completed.ID())
			_, hasTarget := child.Outcome.Target()
			if parent.Outcome.Kind() != invocation.OutcomeExpanded || previous.Outcome.Kind() != invocation.OutcomeOpaque || child.Outcome.Kind() != invocation.OutcomeInterrupted || child.Outcome.Detail() != "context canceled" || hasTarget != resolved || !g.Incomplete() {
				t.Fatal("interruption lost completed facts or ancestor targets")
			}
		})
	}
}

func TestConditionIsIndependentOfResolution(t *testing.T) {
	b := builder(t, invocation.EntryWorkflow, invocation.DefaultBudget())
	conditions := []invocation.Condition{invocation.ConditionEnabled, invocation.ConditionSkipped, invocation.ConditionRuntimeDependent}
	for i, condition := range conditions {
		ticket, err := b.Begin(b.RootContext(), reference("entry", strconv.Itoa(i)), invocation.Facts{Uses: "./action", HasUses: true, Condition: condition})
		if err != nil {
			t.Fatal(err)
		}
		opaque(t, b, ticket)
	}
	g := publish(t, b)
	for i, call := range g.Calls() {
		if call.Condition() != conditions[i] || call.Outcome.Kind() != invocation.OutcomeOpaque {
			t.Fatalf("lost condition on %+v", call)
		}
	}
}

func TestInvalidBudgetsAndAdmission(t *testing.T) {
	for _, budget := range []invocation.Budget{{}, {MaxDepth: 1}, {MaxCalls: 1}, {MaxDepth: -1, MaxCalls: 1}} {
		if _, err := invocation.NewBuilder(invocation.EntryWorkflow, reference("entry", "document"), budget); !errors.Is(err, invocation.ErrInvalidBudget) {
			t.Errorf("accepted invalid budget: %+v", budget)
		}
	}
	b := builder(t, invocation.EntryWorkflow, invocation.DefaultBudget())
	if _, err := b.Begin(invocation.Context{}, reference("entry", "call"), invocation.Facts{Uses: "./x", HasUses: true, Condition: invocation.ConditionEnabled}); !errors.Is(err, invocation.ErrForeignContext) {
		t.Fatalf("unknown parent: %v", err)
	}
	if _, err := b.Begin(b.RootContext(), reference("entry", "call"), invocation.Facts{Uses: "./x", HasUses: true, Condition: invocation.ConditionInvalid}); !errors.Is(err, invocation.ErrInvalidCondition) {
		t.Fatalf("invalid condition: %v", err)
	}
	first := begin(t, b, b.RootID(), reference("entry", "call"))
	if _, err := b.Publish(); !errors.Is(err, invocation.ErrUnfinished) {
		t.Fatalf("published pending reservation: %v", err)
	}
	firstContext, admitted := first.Context()
	if !admitted {
		t.Fatal("first ticket was not admitted")
	}
	if _, err := b.Resolve(firstContext, invocation.Outcome{}); !errors.Is(err, invocation.ErrInvalidOutcome) {
		t.Fatalf("accepted missing resolution: %v", err)
	}
	opaque(t, b, first)
	publish(t, b)
	if _, err := b.Begin(b.RootContext(), reference("entry", "later"), invocation.Facts{Uses: "./later", HasUses: true, Condition: invocation.ConditionEnabled}); !errors.Is(err, invocation.ErrPublished) {
		t.Fatalf("mutated published graph: %v", err)
	}
}
