package invocationview

import (
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	"actionlint.kjanat.dev/v2/source"
)

func ref(document, node string) source.Reference {
	return source.Reference{Document: document, Revision: "revision", Node: node, Address: node}
}

func begin(t *testing.T, b *invocation.Builder, owner invocation.Context, site source.Reference, uses string) invocation.Context {
	t.Helper()
	ticket, err := b.Begin(owner, site, invocation.Facts{
		Step: invocation.Position{Line: 1, Column: 1}, Uses: uses, HasUses: true, Condition: invocation.ConditionEnabled,
	})
	if err != nil || ticket.Status() != invocation.Admitted {
		t.Fatalf("Begin(%q): ticket=%+v err=%v", site.Node, ticket, err)
	}
	context, ok := ticket.Context()
	if !ok {
		t.Fatalf("Begin(%q) returned no context", site.Node)
	}
	return context
}

func mustOutcome(t *testing.T, outcome invocation.Outcome, err error) invocation.Outcome {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return outcome
}

func expanded(t *testing.T, target source.Reference) invocation.Outcome {
	t.Helper()
	outcome, err := invocation.Expanded(target)
	return mustOutcome(t, outcome, err)
}

func publish(t *testing.T, b *invocation.Builder) invocation.Graph {
	t.Helper()
	graph, err := b.Publish()
	if err != nil {
		t.Fatal(err)
	}
	return graph
}

func TestProjectionRepeatedDeclarationsKeepOccurrenceAncestry(t *testing.T) {
	rootRef, outerRef, innerRef := ref("workflow.yml", "root"), ref("outer/action.yml", "outer"), ref("inner/action.yml", "inner")
	b, err := invocation.NewBuilder(invocation.EntryWorkflow, rootRef, invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	var innerContexts []invocation.Context
	for _, n := range []string{"outer-one", "outer-two"} {
		outer := begin(t, b, b.RootContext(), ref("workflow.yml", n), "owner/outer")
		if _, err := b.Resolve(outer, expanded(t, outerRef)); err != nil {
			t.Fatal(err)
		}
		inner := begin(t, b, outer, ref("outer/action.yml", "call-inner"), "owner/inner")
		if _, err := b.Resolve(inner, expanded(t, innerRef)); err != nil {
			t.Fatal(err)
		}
		if _, err := b.AddScript(inner, ref("inner/action.yml", "run"), invocation.ScriptRun, ""); err != nil {
			t.Fatal(err)
		}
		if err := b.Close(inner); err != nil {
			t.Fatal(err)
		}
		if err := b.Close(outer); err != nil {
			t.Fatal(err)
		}
		innerContexts = append(innerContexts, inner)
	}
	graph, err := New(publish(t, b))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := graph.Snapshot()
	if snapshot == nil || len(snapshot.Calls) != 4 || len(snapshot.Scripts) != 2 {
		t.Fatalf("graph=%+v", snapshot)
	}
	first, second := snapshot.Scripts[0], snapshot.Scripts[1]
	if first.ID == second.ID || first.Owner == second.Owner || first.Declaration != second.Declaration || first.Depth != 2 || second.Depth != 2 {
		t.Fatalf("occurrences collapsed: first=%+v second=%+v", first, second)
	}
	if len(first.Ancestry) != 3 || len(second.Ancestry) != 3 || first.Ancestry[0] != snapshot.Entry.ID || first.Ancestry[1] == second.Ancestry[1] || first.Ancestry[2] != first.Owner || second.Ancestry[2] != second.Owner {
		t.Fatalf("ancestry lost occurrence ownership: first=%v second=%v", first.Ancestry, second.Ancestry)
	}
	if first.Owner != analyzer.OccurrenceID(innerContexts[0].ID()) || second.Owner != analyzer.OccurrenceID(innerContexts[1].ID()) {
		t.Fatalf("owners=%q,%q", first.Owner, second.Owner)
	}
}

func TestProjectionRootScriptAndScopesOnlyOccurrences(t *testing.T) {
	entry := ref("action.yml", "entry")
	b, err := invocation.NewBuilder(invocation.EntryAction, entry, invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	id, err := b.AddScript(b.RootContext(), ref("action.yml", "run"), invocation.ScriptRun, "")
	if err != nil {
		t.Fatal(err)
	}
	view, err := New(publish(t, b))
	if err != nil {
		t.Fatal(err)
	}
	script, ok := view.Script(id)
	if !ok || script.Depth != 0 || script.Owner != view.Snapshot().Entry.ID || len(script.Ancestry) != 1 || script.Ancestry[0] != script.Owner {
		t.Fatalf("root script=%+v found=%v", script, ok)
	}
	if _, ok := view.Script(invocation.ID("unknown")); ok {
		t.Fatal("unknown script occurrence found")
	}
	if _, err := New(invocation.Graph{}); !errors.Is(err, analyzer.ErrInvalidCallGraph) {
		t.Fatalf("zero graph error=%v", err)
	}
}

func TestProjectionAllCallOutcomesFromBuilder(t *testing.T) {
	target := ref("target/action.yml", "action")
	tests := []struct {
		name  string
		want  analyzer.CallResolution
		build func(*testing.T, *invocation.Builder)
	}{
		{"expanded", analyzer.ExpandedCall{}, func(t *testing.T, b *invocation.Builder) {
			ctx := begin(t, b, b.RootContext(), ref("workflow.yml", "call"), "owner/action")
			if _, err := b.Resolve(ctx, expanded(t, target)); err != nil {
				t.Fatal(err)
			}
			if err := b.Close(ctx); err != nil {
				t.Fatal(err)
			}
		}},
		{"opaque", analyzer.OpaqueCall{Reason: analyzer.NonComposite, Detail: "opaque"}, func(t *testing.T, b *invocation.Builder) {
			ctx := begin(t, b, b.RootContext(), ref("workflow.yml", "call"), "owner/action")
			o, err := invocation.Opaque(invocation.ReasonNonComposite, "opaque")
			if _, err = b.Resolve(ctx, mustOutcome(t, o, err)); err != nil {
				t.Fatal(err)
			}
		}},
		{"unavailable", analyzer.UnavailableCall{Reason: analyzer.NonLocal, Detail: "unavailable"}, func(t *testing.T, b *invocation.Builder) {
			ctx := begin(t, b, b.RootContext(), ref("workflow.yml", "call"), "owner/action")
			o, err := invocation.Unavailable(invocation.ReasonNonLocal, "unavailable")
			if _, err = b.Resolve(ctx, mustOutcome(t, o, err)); err != nil {
				t.Fatal(err)
			}
		}},
		{"unresolved", analyzer.UnresolvedCall{Reason: analyzer.MissingManifest, Detail: "unresolved"}, func(t *testing.T, b *invocation.Builder) {
			ctx := begin(t, b, b.RootContext(), ref("workflow.yml", "call"), "owner/action")
			o, err := invocation.Unresolved(invocation.ReasonMissingManifest, "unresolved")
			if _, err = b.Resolve(ctx, mustOutcome(t, o, err)); err != nil {
				t.Fatal(err)
			}
		}},
		{"dynamic", analyzer.DynamicCall{Detail: "dynamic"}, func(t *testing.T, b *invocation.Builder) {
			ctx := begin(t, b, b.RootContext(), ref("workflow.yml", "call"), "${{ inputs.action }}")
			o, err := invocation.Dynamic("dynamic")
			if _, err = b.Resolve(ctx, mustOutcome(t, o, err)); err != nil {
				t.Fatal(err)
			}
		}},
		{"cycle", analyzer.CycleCall{}, func(t *testing.T, b *invocation.Builder) {
			ctx := begin(t, b, b.RootContext(), ref("action.yml", "recurse"), "owner/action")
			if _, err := b.Resolve(ctx, expanded(t, ref("action.yml", "entry"))); err != nil {
				t.Fatal(err)
			}
		}},
		{"depth-limited", analyzer.DepthLimitedCall{}, func(t *testing.T, b *invocation.Builder) {
			outer := begin(t, b, b.RootContext(), ref("workflow.yml", "outer"), "owner/outer")
			if _, err := b.Resolve(outer, expanded(t, target)); err != nil {
				t.Fatal(err)
			}
			ticket, err := b.Begin(outer, ref("target/action.yml", "inner"), invocation.Facts{Condition: invocation.ConditionEnabled})
			if err != nil || ticket.Status() != invocation.DepthLimited {
				t.Fatalf("depth frontier ticket=%+v err=%v", ticket, err)
			}
			if err := b.Close(outer); err != nil {
				t.Fatal(err)
			}
		}},
		{"interrupted", analyzer.InterruptedCall{Reason: analyzer.OperationalFailure, Detail: "interrupted", Target: analyzer.Present(target)}, func(t *testing.T, b *invocation.Builder) {
			ctx := begin(t, b, b.RootContext(), ref("workflow.yml", "call"), "owner/action")
			if _, err := b.Resolve(ctx, expanded(t, target)); err != nil {
				t.Fatal(err)
			}
			script, err := b.AddScript(ctx, ref("target/action.yml", "run"), invocation.ScriptRun, "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := b.Interrupt(invocation.ReasonOperational, "interrupted"); err != nil {
				t.Fatal(err)
			}
			if script == "" {
				t.Fatal("script occurrence identity is empty")
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := ref("workflow.yml", "root")
			kind := invocation.EntryWorkflow
			if tt.name == "cycle" {
				entry, kind = ref("action.yml", "entry"), invocation.EntryAction
			}
			budget := invocation.DefaultBudget()
			if tt.name == "depth-limited" {
				budget.MaxDepth = 1
			}
			b, err := invocation.NewBuilder(kind, entry, budget)
			if err != nil {
				t.Fatal(err)
			}
			tt.build(t, b)
			view, err := New(publish(t, b))
			if err != nil {
				t.Fatal(err)
			}
			snapshot := view.Snapshot()
			calls := snapshot.Calls
			if len(calls) != 1 && tt.name != "depth-limited" {
				t.Fatalf("calls=%d", len(calls))
			}
			var got analyzer.CallResolution
			if tt.name == "depth-limited" {
				got = calls[1].Outcome.Value()
			} else {
				got = calls[0].Outcome.Value()
			}
			wantComplete := tt.name == "expanded" || tt.name == "opaque"
			if snapshot.Complete != wantComplete {
				t.Fatalf("complete=%v want %v", snapshot.Complete, wantComplete)
			}
			switch want := tt.want.(type) {
			case analyzer.CycleCall:
				cycle, ok := got.(analyzer.CycleCall)
				if !ok || cycle.Target != entry || cycle.Active != entry || cycle.Closing != calls[0].ID || cycle.Owner != calls[0].Parent {
					t.Fatalf("cycle=%+v got=%T", cycle, got)
				}
			case analyzer.InterruptedCall:
				interrupted, ok := got.(analyzer.InterruptedCall)
				target, present := interrupted.Target.Get()
				if !ok || interrupted.Reason != want.Reason || interrupted.Detail != want.Detail || !present || target != ref("target/action.yml", "action") {
					t.Fatalf("interrupted=%+v target=%+v present=%v", interrupted, target, present)
				}
				expectedID, err := invocation.NewScriptID(invocation.ID(calls[0].ID), ref("target/action.yml", "run"), invocation.ScriptRun, "")
				if err != nil {
					t.Fatal(err)
				}
				if len(snapshot.Scripts) != 1 || snapshot.Scripts[0].ID != analyzer.OccurrenceID(expectedID) || snapshot.Scripts[0].Owner != calls[0].ID || snapshot.Scripts[0].Declaration != ref("target/action.yml", "run") {
					t.Fatalf("interrupted script occurrence lost: %+v", snapshot.Scripts)
				}
			case analyzer.ExpandedCall:
				if expanded, ok := got.(analyzer.ExpandedCall); !ok || expanded.Target != target {
					t.Fatalf("expanded=%+v", got)
				}
			case analyzer.OpaqueCall:
				opaque, ok := got.(analyzer.OpaqueCall)
				if !ok || opaque.Reason != want.Reason || opaque.Detail != want.Detail {
					t.Fatalf("opaque=%+v", got)
				}
			case analyzer.UnavailableCall:
				unavailable, ok := got.(analyzer.UnavailableCall)
				if !ok || unavailable.Reason != want.Reason || unavailable.Detail != want.Detail {
					t.Fatalf("unavailable=%+v", got)
				}
			case analyzer.UnresolvedCall:
				unresolved, ok := got.(analyzer.UnresolvedCall)
				if !ok || unresolved.Reason != want.Reason || unresolved.Detail != want.Detail {
					t.Fatalf("unresolved=%+v", got)
				}
			case analyzer.DynamicCall:
				dynamic, ok := got.(analyzer.DynamicCall)
				if !ok || dynamic.Detail != want.Detail {
					t.Fatalf("dynamic=%+v", got)
				}
			case analyzer.DepthLimitedCall:
				if limited, ok := got.(analyzer.DepthLimitedCall); !ok || limited.Max != 1 {
					t.Fatalf("limited=%+v", got)
				}
			default:
				t.Fatalf("unhandled expectation %T", want)
			}
		})
	}
}

func TestProjectionBudgetStopsAndEntryInterruption(t *testing.T) {
	t.Run("calls", func(t *testing.T) {
		budget := invocation.Budget{MaxDepth: 3, MaxCalls: 1, MaxScripts: 2}
		b, err := invocation.NewBuilder(invocation.EntryWorkflow, ref("workflow.yml", "root"), budget)
		if err != nil {
			t.Fatal(err)
		}
		ctx := begin(t, b, b.RootContext(), ref("workflow.yml", "first"), "owner/a")
		opaque, err := invocation.Opaque(invocation.ReasonNonComposite, "")
		if _, err := b.Resolve(ctx, mustOutcome(t, opaque, err)); err != nil {
			t.Fatal(err)
		}
		ticket, err := b.Begin(b.RootContext(), ref("workflow.yml", "second"), invocation.Facts{Condition: invocation.ConditionEnabled})
		if err != nil || ticket.Status() != invocation.Stopped {
			t.Fatalf("ticket=%+v err=%v", ticket, err)
		}
		view, err := New(publish(t, b))
		if err != nil {
			t.Fatal(err)
		}
		stop, ok := view.Snapshot().Stop.Get()
		if !ok || stop.Limit != analyzer.CallLimit || stop.Max != 1 || stop.Site.Node != "second" {
			t.Fatalf("stop=%+v present=%v", stop, ok)
		}
	})
	t.Run("scripts", func(t *testing.T) {
		budget := invocation.Budget{MaxDepth: 3, MaxCalls: 2, MaxScripts: 1}
		b, err := invocation.NewBuilder(invocation.EntryWorkflow, ref("workflow.yml", "root"), budget)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := b.AddScript(b.RootContext(), ref("workflow.yml", "first"), invocation.ScriptRun, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := b.AddScript(b.RootContext(), ref("workflow.yml", "second"), invocation.ScriptRun, ""); !errors.Is(err, invocation.ErrScriptBudget) {
			t.Fatalf("second script err=%v", err)
		}
		view, err := New(publish(t, b))
		if err != nil {
			t.Fatal(err)
		}
		stop, ok := view.Snapshot().Stop.Get()
		if !ok || stop.Limit != analyzer.ScriptLimit || stop.Max != 1 || stop.Site.Node != "second" {
			t.Fatalf("stop=%+v present=%v", stop, ok)
		}
	})
	t.Run("entry interruption", func(t *testing.T) {
		b, err := invocation.NewBuilder(invocation.EntryWorkflow, ref("workflow.yml", "root"), invocation.DefaultBudget())
		if err != nil {
			t.Fatal(err)
		}
		if err := b.InterruptEntry(invocation.ReasonCanceled, "stopped"); err != nil {
			t.Fatal(err)
		}
		view, err := New(publish(t, b))
		if err != nil {
			t.Fatal(err)
		}
		snapshot := view.Snapshot()
		interruption, ok := snapshot.EntryInterruption.Get()
		if !ok || interruption.Reason != analyzer.Canceled || interruption.Detail != "stopped" || snapshot.Complete {
			t.Fatalf("snapshot=%+v", snapshot)
		}
	})
}

func TestProjectionSnapshotsAndScriptAccessorsAreDetached(t *testing.T) {
	entry := ref("workflow.yml", "root")
	b, err := invocation.NewBuilder(invocation.EntryWorkflow, entry, invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	call := begin(t, b, b.RootContext(), ref("workflow.yml", "call"), "owner/action")
	if _, err := b.Resolve(call, expanded(t, ref("action.yml", "target"))); err != nil {
		t.Fatal(err)
	}
	scriptID, err := b.AddScript(call, ref("action.yml", "run"), invocation.ScriptRun, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Close(call); err != nil {
		t.Fatal(err)
	}
	view, err := New(publish(t, b))
	if err != nil {
		t.Fatal(err)
	}
	first := view.Snapshot()
	first.Calls[0].Ancestry[0] = "corrupt-call"
	first.Scripts[0].Ancestry[0] = "corrupt-script"
	first.Scripts = append(first.Scripts, analyzer.ScriptOccurrence{ID: "extra"})
	accessor, ok := view.Script(scriptID)
	if !ok {
		t.Fatal("registered script missing")
	}
	accessor.Ancestry[0] = "corrupt-accessor"
	accessor.Ancestry = append(accessor.Ancestry, "extra")
	second := view.Snapshot()
	if len(second.Scripts) != 1 || second.Calls[0].Ancestry[0] != second.Entry.ID || second.Scripts[0].Ancestry[0] != second.Entry.ID {
		t.Fatalf("projection changed through returned values: %+v", second)
	}
	if second.Calls[0].Ancestry[0] == second.Scripts[0].Ancestry[0] && &second.Calls[0].Ancestry[0] == &second.Scripts[0].Ancestry[0] {
		t.Fatal("records unexpectedly share mutable ancestry storage")
	}
}

func TestProjectionRejectsUnknownEnumsAndInvalidPublicOutcomes(t *testing.T) {
	if _, err := projectCondition(invocation.Condition(255)); !errors.Is(err, analyzer.ErrInvalidCallGraph) {
		t.Fatalf("condition error=%v", err)
	}
	if _, err := projectLimit(invocation.LimitKind(255)); !errors.Is(err, analyzer.ErrInvalidCallGraph) {
		t.Fatalf("limit error=%v", err)
	}
	for _, value := range []analyzer.CallResolution{nil, (*analyzer.ExpandedCall)(nil), analyzer.ExpandedCall{}, analyzer.OpaqueCall{}, analyzer.UnavailableCall{}, analyzer.UnresolvedCall{}, analyzer.DepthLimitedCall{}} {
		if _, err := analyzer.NewCallOutcome(value); !errors.Is(err, analyzer.ErrInvalidCallGraph) {
			t.Errorf("NewCallOutcome(%T) error=%v", value, err)
		}
	}
	if _, err := analyzer.NewCallOutcome(analyzer.ExpandedCall{Target: ref("target.yml", "action")}); err != nil {
		t.Fatal(err)
	}
}
