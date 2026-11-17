package component

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	bind "actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint/invocationcontext"
)

var invocationContextEntry = sourcepos.Reference{Document: "workflow.yml", Revision: "revision", Node: "document"}

func invocationContextBuilder(t *testing.T, budget invocation.Budget) *invocation.Builder {
	t.Helper()
	builder, err := invocation.NewBuilder(invocation.EntryWorkflow, invocationContextEntry, budget)
	if err != nil {
		t.Fatal(err)
	}
	return builder
}

func publishInvocationContext(t *testing.T, builder *invocation.Builder) invocation.Graph {
	t.Helper()
	graph, err := builder.Publish()
	if err != nil {
		t.Fatal(err)
	}
	return graph
}

func invocationContextGraph(t *testing.T) invocation.Graph {
	t.Helper()
	return publishInvocationContext(t, invocationContextBuilder(t, invocation.Budget{MaxDepth: 1, MaxCalls: 1, MaxScripts: 1}))
}

func invocationContextFor(graph invocation.Graph) context.Context {
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
	t.Parallel()
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
		t.Parallel()
		assertUnavailable(t, context.Background(), "invocation graph is unavailable")
	})
	t.Run("canceled context", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(invocationContextFor(invocationContextGraph(t)))
		cancel()
		assertUnavailable(t, ctx, context.Canceled.Error())
	})
}

func TestInvocationContextValidEmptyGraph(t *testing.T) {
	t.Parallel()
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

// invocationContextPagedGraph nests count calls, each expanded into its own
// body, and registers count scripts in the deepest body.
func invocationContextPagedGraph(t *testing.T, count int) (invocation.Graph, []invocation.ID, []invocation.ID) {
	t.Helper()
	builder := invocationContextBuilder(t, invocation.Budget{MaxDepth: count, MaxCalls: count, MaxScripts: count})
	owner, contexts := builder.RootContext(), make([]invocation.Context, 0, count)
	callIDs, scriptIDs := make([]invocation.ID, 0, count), make([]invocation.ID, 0, count)
	at := func(node string) sourcepos.Reference {
		return sourcepos.Reference{Document: invocationContextEntry.Document, Revision: invocationContextEntry.Revision, Node: node}
	}
	for index := range count {
		ticket, err := builder.Begin(owner, at(fmt.Sprintf("call-%d", index)), invocation.Facts{Step: sourcepos.Position{Line: 1, Column: 1}, Condition: invocation.ConditionEnabled})
		if err != nil {
			t.Fatal(err)
		}
		call, admitted := ticket.Context()
		expanded, err := invocation.Expanded(at(fmt.Sprintf("body-%d", index)))
		if !admitted || err != nil {
			t.Fatalf("call %d not admitted: %v", index, err)
		}
		if _, err := builder.Resolve(call, expanded); err != nil {
			t.Fatal(err)
		}
		callIDs, contexts, owner = append(callIDs, call.ID()), append(contexts, call), call
	}
	for index := range count {
		id, err := builder.AddScript(owner, at(fmt.Sprintf("script-%d", index)), invocation.ScriptRun, "")
		if err != nil {
			t.Fatal(err)
		}
		scriptIDs = append(scriptIDs, id)
	}
	for _, context := range slices.Backward(contexts) {
		if err := builder.Close(context); err != nil {
			t.Fatal(err)
		}
	}
	return publishInvocationContext(t, builder), callIDs, scriptIDs
}

func TestInvocationContextPagesAndLookupsRetainOrderAndDetachAncestry(t *testing.T) {
	t.Parallel()
	graph, callIDs, scriptIDs := invocationContextPagedGraph(t, MaxInvocationPageSize+1)
	api, ctx := invocationContext{}, invocationContextFor(graph)
	var calls []bind.Call
	for offset := uint32(0); offset < uint32(len(callIDs)); offset += MaxInvocationPageSize {
		page, ok := requireInvocationResult(api.Calls(ctx, offset, MaxInvocationPageSize)).(bind.ResultListCallStringOk)
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
	for offset := uint32(0); offset < uint32(len(scriptIDs)); offset += MaxInvocationPageSize {
		page, ok := requireInvocationResult(api.Scripts(ctx, offset, MaxInvocationPageSize)).(bind.ResultListScriptOccurrenceStringOk)
		if !ok {
			t.Fatalf("scripts page at %d failed", offset)
		}
		scripts = append(scripts, page.Value...)
	}
	if len(scripts) != len(scriptIDs) {
		t.Fatalf("script count=%d, want %d", len(scripts), len(scriptIDs))
	}
	for i, script := range scripts {
		if script.ID != string(scriptIDs[i]) || script.Depth != uint32(len(callIDs)) || len(script.Ancestry) != len(callIDs)+1 || script.Ancestry[0] != string(graph.RootID()) || script.Ancestry[len(script.Ancestry)-1] != script.Owner {
			t.Fatalf("script %d = %+v", i, script)
		}
	}
	calls[0].Ancestry[0] = "changed-call"
	firstCall, ok := requireInvocationResult(api.CallByID(ctx, string(callIDs[0]))).(bind.ResultOptionCallStringOk)
	if !ok || !firstCall.Value.IsSome || firstCall.Value.Value.Ancestry[0] != string(graph.RootID()) {
		t.Fatal("call page ancestry mutation changed graph")
	}
	wantScriptAncestor := scripts[0].Ancestry[1]
	scripts[0].Ancestry[1] = "changed-script"
	firstScript, ok := requireInvocationResult(api.ScriptByID(ctx, string(scriptIDs[0]))).(bind.ResultOptionScriptOccurrenceStringOk)
	if !ok || !firstScript.Value.IsSome || firstScript.Value.Value.Ancestry[1] != wantScriptAncestor {
		t.Fatal("script page ancestry mutation changed graph")
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
	t.Parallel()
	graph, _, _ := invocationContextPagedGraph(t, MaxInvocationPageSize+1)
	api, ctx := invocationContext{}, invocationContextFor(graph)
	count := uint32(graph.CallCount())
	for _, bounds := range [][2]uint32{{count, 1}, {count + 1, 1}, {0, 0}, {0, MaxInvocationPageSize + 1}, {math.MaxUint32, 1}} {
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
	t.Parallel()
	t.Run("stop", func(t *testing.T) {
		t.Parallel()
		builder := invocationContextBuilder(t, invocation.Budget{MaxDepth: 1, MaxCalls: 1, MaxScripts: 1})
		at := func(node string) sourcepos.Reference {
			return sourcepos.Reference{Document: invocationContextEntry.Document, Revision: invocationContextEntry.Revision, Node: node}
		}
		facts := invocation.Facts{Step: sourcepos.Position{Line: 1, Column: 1}, Condition: invocation.ConditionEnabled}
		ticket, err := builder.Begin(builder.RootContext(), at("call"), facts)
		if err != nil {
			t.Fatal(err)
		}
		call, _ := ticket.Context()
		opaque, err := invocation.Opaque(invocation.ReasonNonComposite, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := builder.Resolve(call, opaque); err != nil {
			t.Fatal(err)
		}
		if ticket, err := builder.Begin(builder.RootContext(), at("rejected"), facts); err != nil || ticket.Status() != invocation.Stopped {
			t.Fatalf("second call was not stopped: %v %v", ticket.Status(), err)
		}
		graph := publishInvocationContext(t, builder)
		summary, ok := requireInvocationResult((invocationContext{}).Describe(invocationContextFor(graph))).(bind.ResultSummaryStringOk)
		if !ok || summary.Value.Complete || !summary.Value.Stop.IsSome || summary.Value.Stop.Value.Limit != bind.StopLimitTotal || summary.Value.Stop.Value.Max != 1 || summary.Value.Stop.Value.Parent != string(graph.RootID()) || summary.Value.Stop.Value.Site.Node != "rejected" {
			t.Fatalf("stop summary: %+v", summary)
		}
	})
	t.Run("entry interruption", func(t *testing.T) {
		t.Parallel()
		builder := invocationContextBuilder(t, invocation.Budget{MaxDepth: 1, MaxCalls: 1, MaxScripts: 1})
		if err := builder.InterruptEntry(invocation.ReasonOperational, "entry expansion failed"); err != nil {
			t.Fatal(err)
		}
		graph := publishInvocationContext(t, builder)
		summary, ok := requireInvocationResult((invocationContext{}).Describe(invocationContextFor(graph))).(bind.ResultSummaryStringOk)
		if !ok || summary.Value.Complete || !summary.Value.EntryInterruption.IsSome || summary.Value.EntryInterruption.Value.Reason != bind.InterruptionReasonOperationalFailure || summary.Value.EntryInterruption.Value.Detail != "entry expansion failed" {
			t.Fatalf("interruption summary: %+v", summary)
		}
	})
}

func TestInvocationContextRejectsBudgetOverflow(t *testing.T) {
	t.Parallel()
	if strconv.IntSize <= 32 {
		t.Skip("int cannot exceed WIT u32 on 32-bit platforms")
	}
	beyondU32 := uint64(math.MaxUint32) + 1
	for name, budget := range map[string]invocation.Budget{
		"depth":   {MaxDepth: int(beyondU32), MaxCalls: 1, MaxScripts: 1},
		"calls":   {MaxDepth: 1, MaxCalls: int(beyondU32), MaxScripts: 1},
		"scripts": {MaxDepth: 1, MaxCalls: 1, MaxScripts: int(beyondU32)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			graph := publishInvocationContext(t, invocationContextBuilder(t, budget))
			requireInvocationError(t, requireInvocationResult((invocationContext{}).Describe(invocationContextFor(graph))), "exceeds WIT integer range")
		})
	}
}
