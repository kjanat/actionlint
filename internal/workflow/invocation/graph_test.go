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
	if err := b.Close(id); err != nil {
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
	if _, err := b.Resolve(ticket.ID(), o); err != nil {
		t.Fatal(err)
	}
}

func TestRepeatedCallsRetainExactAncestry(t *testing.T) {
	b := builder(t, invocation.EntryWorkflow, invocation.DefaultBudget())
	var branches [][]invocation.ID
	var sites [][]source.Reference
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
			if call.Site != sites[branch][i] || call.Parent != chain[i] || call.Depth != i+1 {
				t.Errorf("branch %d edge %d lost its source or parent: %+v", branch, i, call)
			}
		}
	}
	left, _ := g.Call(branches[0][3])
	right, _ := g.Call(branches[1][3])
	leftTarget, _ := left.Outcome.Target()
	rightTarget, _ := right.Outcome.Target()
	if left.ID == right.ID || left.Site != right.Site || leftTarget != rightTarget {
		t.Fatal("repeated declaration must retain source identity and have distinct occurrences")
	}
	leftScript, err := g.ScriptID(left.ID, reference("inner", "run"), invocation.ScriptRun, "")
	if err != nil {
		t.Fatal(err)
	}
	rightScript, err := g.ScriptID(right.ID, reference("inner", "run"), invocation.ScriptRun, "")
	if err != nil || rightScript == leftScript {
		t.Fatalf("repeated scripts share occurrence identity: %v", err)
	}
	mutated := g.Calls()
	mutated[0].Site.Document = "corrupted"
	stored, _ := g.Call(branches[0][1])
	if stored.Site.Document != "entry" {
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
	outcome, err := b.Resolve(closing.ID(), target)
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
				b := builder(t, kind, invocation.Budget{MaxDepth: maxDepth, MaxCalls: 100})
				parent := b.RootID()
				var expanded []invocation.ID
				for depth := 1; depth <= maxDepth; depth++ {
					ticket := begin(t, b, parent, reference(strconv.Itoa(depth-1), "call"))
					expand(t, b, ticket, reference(strconv.Itoa(depth), "document"))
					expanded = append(expanded, ticket.ID())
					parent = ticket.ID()
				}
				frontier := begin(t, b, parent, reference("frontier", "call"))
				if frontier.Status() != invocation.DepthLimited {
					t.Fatal("depth frontier requested metadata lookup")
				}
				if _, err := b.Resolve(frontier.ID(), invocation.Outcome{}); !errors.Is(err, invocation.ErrNotPending) {
					t.Fatalf("frontier can be resolved: %v", err)
				}
				for _, e := range slices.Backward(expanded) {
					closeCall(t, b, e)
				}
				g := publish(t, b)
				call, ok := g.Call(frontier.ID())
				limit, maximum, limited := call.Outcome.Limit()
				if !ok || call.Depth != maxDepth+1 || !limited || limit != invocation.LimitDepth || maximum != maxDepth || !g.Incomplete() {
					t.Fatalf("wrong depth frontier: %+v", call)
				}
			})
		}
	}
}

func TestTotalBudgetRetainsOneBoundary(t *testing.T) {
	b := builder(t, invocation.EntryWorkflow, invocation.Budget{MaxDepth: 10, MaxCalls: 2})
	for i := range 2 {
		opaque(t, b, begin(t, b, b.RootID(), reference("entry", strconv.Itoa(i))))
	}
	for i := 2; i < 102; i++ {
		ticket := begin(t, b, b.RootID(), reference("entry", strconv.Itoa(i)))
		if ticket.Status() != invocation.Stopped || ticket.ID() != "" {
			t.Fatal("total budget allocated a rejected call")
		}
	}
	if _, err := b.Begin(b.RootID(), reference("entry", "0"), "./duplicate", invocation.ConditionEnabled); !errors.Is(err, invocation.ErrDuplicateCall) {
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
			id, err := b.Interrupt(invocation.ReasonCanceled, "context canceled")
			if err != nil || id != inner.ID() {
				t.Fatalf("interrupted %s: %v", id, err)
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
		ticket, err := b.Begin(b.RootID(), reference("entry", strconv.Itoa(i)), "./action", condition)
		if err != nil {
			t.Fatal(err)
		}
		opaque(t, b, ticket)
	}
	g := publish(t, b)
	for i, call := range g.Calls() {
		if call.Condition != conditions[i] || call.Outcome.Kind() != invocation.OutcomeOpaque {
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
	if _, err := b.Begin("unknown", reference("entry", "call"), "./x", invocation.ConditionEnabled); !errors.Is(err, invocation.ErrUnknownParent) {
		t.Fatalf("unknown parent: %v", err)
	}
	if _, err := b.Begin(b.RootID(), reference("entry", "call"), "./x", invocation.ConditionInvalid); !errors.Is(err, invocation.ErrInvalidCondition) {
		t.Fatalf("invalid condition: %v", err)
	}
	first := begin(t, b, b.RootID(), reference("entry", "call"))
	if _, err := b.Publish(); !errors.Is(err, invocation.ErrUnfinished) {
		t.Fatalf("published pending reservation: %v", err)
	}
	if _, err := b.Resolve(first.ID(), invocation.Outcome{}); !errors.Is(err, invocation.ErrInvalidOutcome) {
		t.Fatalf("accepted missing resolution: %v", err)
	}
	opaque(t, b, first)
	publish(t, b)
	if _, err := b.Begin(b.RootID(), reference("entry", "later"), "./later", invocation.ConditionEnabled); !errors.Is(err, invocation.ErrPublished) {
		t.Fatalf("mutated published graph: %v", err)
	}
}
