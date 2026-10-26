package component

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	bind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/invocationcontext"
	"actionlint.kjanat.dev/v2/source"
)

func invocationContextGraph(t *testing.T) analyzer.CallGraph {
	t.Helper()
	graph, err := analyzer.NewCallGraph(analyzer.CallGraphData{
		Entry: analyzer.InvocationEntry{
			ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Kind: analyzer.WorkflowEntry,
			Reference: source.Reference{Document: "workflow.yml", Revision: "revision", Node: "document"},
		},
		Budget: analyzer.InvocationBudget{MaxDepth: 1, MaxCalls: 1, MaxScripts: 1}, Complete: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return graph
}

func invocationContextFor(graph analyzer.CallGraph) context.Context {
	return context.WithValue(context.Background(), invocationGraphKey{}, graph)
}

func invocationResultError(value any) (string, bool) {
	switch result := value.(type) {
	case bind.ResultSummaryStringErr:
		return result.Value, true
	case bind.ResultListCallStringErr:
		return result.Value, true
	case bind.ResultListScriptOccurrenceStringErr:
		return result.Value, true
	case bind.ResultOptionCallStringErr:
		return result.Value, true
	case bind.ResultOptionScriptOccurrenceStringErr:
		return result.Value, true
	default:
		return "", false
	}
}

func requireInvocationError(t *testing.T, value any, want string) {
	t.Helper()
	got, ok := invocationResultError(value)
	if !ok || !strings.Contains(got, want) {
		t.Fatalf("result error = %q, %v; want containing %q", got, ok, want)
	}
}

func requireInvocationResult[T any](value T, err error) T {
	if err != nil {
		panic(fmt.Sprintf("invocation context returned Go error: %v", err))
	}
	return value
}

func TestInvocationContextUnavailableOutsideCallAndWhenCanceled(t *testing.T) {
	api := invocationContext{}
	assertUnavailable := func(t *testing.T, ctx context.Context, want string) {
		t.Helper()
		results := []any{
			requireInvocationResult(api.Describe(ctx)),
			requireInvocationResult(api.Calls(ctx, 0, 1)),
			requireInvocationResult(api.Scripts(ctx, 0, 1)),
			requireInvocationResult(api.CallByID(ctx, "unknown")),
			requireInvocationResult(api.ScriptByID(ctx, "unknown")),
		}
		for _, result := range results {
			requireInvocationError(t, result, want)
		}
	}
	t.Run("missing graph", func(t *testing.T) {
		assertUnavailable(t, context.Background(), "invocation graph is unavailable")
	})
	t.Run("canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(invocationContextFor(invocationContextGraph(t)))
		cancel()
		assertUnavailable(t, ctx, context.Canceled.Error())
	})
}

func TestInvocationContextValidEmptyGraph(t *testing.T) {
	api, ctx := invocationContext{}, invocationContextFor(invocationContextGraph(t))
	summaryResult, ok := requireInvocationResult(api.Describe(ctx)).(bind.ResultSummaryStringOk)
	if !ok {
		t.Fatal("empty graph summary unavailable")
	}
	summary := summaryResult.Value
	if summary.Entry.Kind != bind.EntryKindWorkflow || summary.Entry.Reference.Document != "workflow.yml" || summary.Budget != (bind.Budget{MaxDepth: 1, MaxCalls: 1, MaxScripts: 1}) || summary.Calls != 0 || summary.Scripts != 0 || !summary.Complete || summary.Stop.IsSome || summary.EntryInterruption.IsSome {
		t.Fatalf("empty graph summary: %+v", summary)
	}
	calls, ok := requireInvocationResult(api.Calls(ctx, 0, 1)).(bind.ResultListCallStringOk)
	if !ok || len(calls.Value) != 0 {
		t.Fatalf("empty calls: %+v", calls)
	}
	scripts, ok := requireInvocationResult(api.Scripts(ctx, 0, 1)).(bind.ResultListScriptOccurrenceStringOk)
	if !ok || len(scripts.Value) != 0 {
		t.Fatalf("empty scripts: %+v", scripts)
	}
	call, ok := requireInvocationResult(api.CallByID(ctx, "missing")).(bind.ResultOptionCallStringOk)
	if !ok || call.Value.IsSome {
		t.Fatalf("unknown call lookup: %+v", call)
	}
	script, ok := requireInvocationResult(api.ScriptByID(ctx, "missing")).(bind.ResultOptionScriptOccurrenceStringOk)
	if !ok || script.Value.IsSome {
		t.Fatalf("unknown script lookup: %+v", script)
	}
}

func invocationContextPagedGraph(t *testing.T, count int) (analyzer.CallGraph, []analyzer.OccurrenceID, []analyzer.OccurrenceID) {
	t.Helper()
	entryID := analyzer.OccurrenceID(strings.Repeat("f", 64))
	entry := source.Reference{Document: "workflow.yml", Revision: "revision", Node: "document"}
	body := source.Reference{Document: entry.Document, Revision: entry.Revision, Node: "body"}
	outcome, err := analyzer.NewCallOutcome(analyzer.ExpandedCall{Target: body})
	if err != nil {
		t.Fatal(err)
	}
	data := analyzer.CallGraphData{
		Entry:  analyzer.InvocationEntry{ID: entryID, Kind: analyzer.WorkflowEntry, Reference: entry},
		Budget: analyzer.InvocationBudget{MaxDepth: count, MaxCalls: count, MaxScripts: count}, Complete: true,
	}
	callIDs := make([]analyzer.OccurrenceID, 0, count)
	ancestry := []analyzer.OccurrenceID{entryID}
	parent := entryID
	for index := range count {
		id := analyzer.OccurrenceID(fmt.Sprintf("%064x", index+1))
		callIDs = append(callIDs, id)
		data.Calls = append(data.Calls, analyzer.Call{
			ID: id, Parent: parent, Ancestry: append([]analyzer.OccurrenceID(nil), ancestry...), Depth: index + 1,
			Site: source.Reference{Document: body.Document, Revision: body.Revision, Node: fmt.Sprintf("call-%d", index)},
			Step: diagnostic.Position{Line: 1, Column: 1}, Condition: analyzer.CallEnabled, Outcome: outcome,
		})
		ancestry = append(ancestry, id)
		parent = id
	}
	scriptIDs := make([]analyzer.OccurrenceID, 0, count)
	for index := range count {
		id := analyzer.OccurrenceID(fmt.Sprintf("%064x", index+10000))
		scriptIDs = append(scriptIDs, id)
		data.Scripts = append(data.Scripts, analyzer.ScriptOccurrence{
			ID: id, Owner: parent, Ancestry: append([]analyzer.OccurrenceID(nil), ancestry...), Depth: count,
			Declaration: source.Reference{Document: body.Document, Revision: body.Revision, Node: fmt.Sprintf("script-%d", index)}, Kind: analyzer.RunScript,
		})
	}
	graph, err := analyzer.NewCallGraph(data)
	if err != nil {
		t.Fatal(err)
	}
	return graph, callIDs, scriptIDs
}

func TestInvocationContextPagesAndLookupsRetainOrderAndDetachAncestry(t *testing.T) {
	graph, callIDs, scriptIDs := invocationContextPagedGraph(t, analyzer.MaxCallGraphPageSize+1)
	api, ctx := invocationContext{}, invocationContextFor(graph)
	var calls []bind.Call
	for offset := uint32(0); offset < uint32(len(callIDs)); offset += analyzer.MaxCallGraphPageSize {
		page, ok := requireInvocationResult(api.Calls(ctx, offset, analyzer.MaxCallGraphPageSize)).(bind.ResultListCallStringOk)
		if !ok {
			t.Fatalf("calls page at %d failed", offset)
		}
		calls = append(calls, page.Value...)
	}
	if len(calls) != len(callIDs) {
		t.Fatalf("call count=%d, want %d", len(calls), len(callIDs))
	}
	for i, call := range calls {
		if call.ID != string(callIDs[i]) || call.Depth != uint32(i+1) {
			t.Fatalf("call %d = %+v", i, call)
		}
	}
	var scripts []bind.ScriptOccurrence
	for offset := uint32(0); offset < uint32(len(scriptIDs)); offset += analyzer.MaxCallGraphPageSize {
		page, ok := requireInvocationResult(api.Scripts(ctx, offset, analyzer.MaxCallGraphPageSize)).(bind.ResultListScriptOccurrenceStringOk)
		if !ok {
			t.Fatalf("scripts page at %d failed", offset)
		}
		scripts = append(scripts, page.Value...)
	}
	if len(scripts) != len(scriptIDs) {
		t.Fatalf("script count=%d, want %d", len(scripts), len(scriptIDs))
	}
	for i, script := range scripts {
		if script.ID != string(scriptIDs[i]) || script.Depth != uint32(len(callIDs)) || len(script.Ancestry) != len(callIDs)+1 || script.Ancestry[0] != string(graph.Entry().ID) || script.Ancestry[len(script.Ancestry)-1] != script.Owner {
			t.Fatalf("script %d = %+v", i, script)
		}
	}
	calls[0].Ancestry[0] = "changed-call"
	firstCall, ok := requireInvocationResult(api.CallByID(ctx, string(callIDs[0]))).(bind.ResultOptionCallStringOk)
	if !ok || !firstCall.Value.IsSome || firstCall.Value.Value.Ancestry[0] != string(graph.Entry().ID) {
		t.Fatal("call page ancestry mutation reached retained graph")
	}
	wantScriptAncestor := scripts[0].Ancestry[1]
	scripts[0].Ancestry[1] = "changed-script"
	firstScript, ok := requireInvocationResult(api.ScriptByID(ctx, string(scriptIDs[0]))).(bind.ResultOptionScriptOccurrenceStringOk)
	if !ok || !firstScript.Value.IsSome || firstScript.Value.Value.Ancestry[1] != wantScriptAncestor {
		t.Fatal("script page ancestry mutation reached retained graph")
	}
	unknownCallResult := requireInvocationResult(api.CallByID(ctx, "unknown"))
	unknownCall, ok := unknownCallResult.(bind.ResultOptionCallStringOk)
	if !ok {
		t.Fatal("unknown call lookup did not return None")
	}
	if unknownCall.Value.IsSome {
		t.Fatal("unknown call lookup returned Some")
	}
	unknownScriptResult := requireInvocationResult(api.ScriptByID(ctx, "unknown"))
	unknownScript, ok := unknownScriptResult.(bind.ResultOptionScriptOccurrenceStringOk)
	if !ok {
		t.Fatal("unknown script lookup did not return None")
	}
	if unknownScript.Value.IsSome {
		t.Fatal("unknown script lookup returned Some")
	}
}

func TestInvocationContextPageBoundsAndNilEndPages(t *testing.T) {
	graph, _, _ := invocationContextPagedGraph(t, analyzer.MaxCallGraphPageSize+1)
	api, ctx := invocationContext{}, invocationContextFor(graph)
	count := uint32(graph.CallCount())
	for _, bounds := range [][2]uint32{{count, 1}, {count + 1, 1}, {0, 0}, {0, analyzer.MaxCallGraphPageSize + 1}, {math.MaxUint32, 1}} {
		calls := requireInvocationResult(api.Calls(ctx, bounds[0], bounds[1]))
		scripts := requireInvocationResult(api.Scripts(ctx, bounds[0], bounds[1]))
		if bounds[0] == count {
			callPage, callOK := calls.(bind.ResultListCallStringOk)
			scriptPage, scriptOK := scripts.(bind.ResultListScriptOccurrenceStringOk)
			if !callOK || len(callPage.Value) != 0 || !scriptOK || len(scriptPage.Value) != 0 {
				t.Fatalf("end pages: calls=%+v scripts=%+v", calls, scripts)
			}
			continue
		}
		requireInvocationError(t, calls, "invalid invocation page bounds")
		requireInvocationError(t, scripts, "invalid invocation page bounds")
	}
}

func TestInvocationContextDescribePreservesStopAndInterruption(t *testing.T) {
	t.Run("stop", func(t *testing.T) {
		base := invocationContextGraph(t).Snapshot()
		callID := analyzer.OccurrenceID(strings.Repeat("b", 64))
		callSite := source.Reference{Document: base.Entry.Reference.Document, Revision: base.Entry.Reference.Revision, Node: "call"}
		outcome, err := analyzer.NewCallOutcome(analyzer.ExpandedCall{Target: source.Reference{Document: callSite.Document, Revision: callSite.Revision, Node: "body"}})
		if err != nil {
			t.Fatal(err)
		}
		base.Calls = []analyzer.Call{{ID: callID, Parent: base.Entry.ID, Ancestry: []analyzer.OccurrenceID{base.Entry.ID}, Depth: 1, Site: callSite, Step: diagnostic.Position{Line: 1, Column: 1}, Condition: analyzer.CallEnabled, Outcome: outcome}}
		base.Stop = analyzer.Present(analyzer.InvocationStop{Parent: base.Entry.ID, Site: callSite, Limit: analyzer.CallLimit, Max: 1})
		base.Complete = false
		graph, err := analyzer.NewCallGraph(base)
		if err != nil {
			t.Fatal(err)
		}
		summary, ok := requireInvocationResult((invocationContext{}).Describe(invocationContextFor(graph))).(bind.ResultSummaryStringOk)
		if !ok || summary.Value.Complete || !summary.Value.Stop.IsSome || summary.Value.Stop.Value.Limit != bind.StopLimitTotal || summary.Value.Stop.Value.Max != 1 || summary.Value.Stop.Value.Parent != string(base.Entry.ID) || summary.Value.Stop.Value.Site.Node != callSite.Node {
			t.Fatalf("stop summary: %+v", summary)
		}
	})
	t.Run("entry interruption", func(t *testing.T) {
		base := invocationContextGraph(t).Snapshot()
		base.EntryInterruption = analyzer.Present(analyzer.Interruption{Reason: analyzer.OperationalFailure, Detail: "entry expansion failed"})
		base.Complete = false
		graph, err := analyzer.NewCallGraph(base)
		if err != nil {
			t.Fatal(err)
		}
		summary, ok := requireInvocationResult((invocationContext{}).Describe(invocationContextFor(graph))).(bind.ResultSummaryStringOk)
		if !ok || summary.Value.Complete || !summary.Value.EntryInterruption.IsSome || summary.Value.EntryInterruption.Value.Reason != bind.InterruptionReasonOperationalFailure || summary.Value.EntryInterruption.Value.Detail != "entry expansion failed" {
			t.Fatalf("interruption summary: %+v", summary)
		}
	})
}

func TestInvocationContextRejectsBudgetOverflow(t *testing.T) {
	if strconv.IntSize <= 32 {
		t.Skip("int cannot exceed WIT u32 on 32-bit platforms")
	}
	const beyondU32 = uint64(math.MaxUint32) + 1
	for name, set := range map[string]func(*analyzer.CallGraphData){
		"depth":   func(data *analyzer.CallGraphData) { data.Budget.MaxDepth = int(beyondU32) },
		"calls":   func(data *analyzer.CallGraphData) { data.Budget.MaxCalls = int(beyondU32) },
		"scripts": func(data *analyzer.CallGraphData) { data.Budget.MaxScripts = int(beyondU32) },
	} {
		t.Run(name, func(t *testing.T) {
			data := invocationContextGraph(t).Snapshot()
			set(&data)
			graph, err := analyzer.NewCallGraph(data)
			if err != nil {
				t.Fatal(err)
			}
			requireInvocationError(t, requireInvocationResult((invocationContext{}).Describe(invocationContextFor(graph))), "exceeds WIT integer range")
		})
	}
}
