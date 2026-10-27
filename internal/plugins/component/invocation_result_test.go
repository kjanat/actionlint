package component

import (
	"context"
	"testing"

	bind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/v0.2.0/invocationcontext"
)

func TestInvocationContextQueriesStayScopedToCall(t *testing.T) {
	var api bind.InvocationContext = invocationContext{}
	graph, calls, scripts := invocationContextPagedGraph(t, 1)
	populated := invocationContextFor(graph)
	empty := invocationContextFor(invocationContextGraph(t))
	for _, tc := range []struct {
		name  string
		ctx   context.Context
		count uint32
	}{
		{"populated call", populated, 1},
		{"following empty call", empty, 0},
		{"original call remains valid", populated, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			summary, ok := requireInvocationResult(api.Describe(tc.ctx)).(bind.ResultSummaryStringOk)
			if !ok || summary.Value.Calls != tc.count || summary.Value.Scripts != tc.count {
				t.Fatalf("call summary = %+v", summary)
			}
			page, ok := requireInvocationResult(api.Calls(tc.ctx, 0, 1)).(bind.ResultListCallStringOk)
			if !ok || len(page.Value) != int(tc.count) {
				t.Fatalf("call page = %+v", page)
			}
			scriptPage, ok := requireInvocationResult(api.Scripts(tc.ctx, 0, 1)).(bind.ResultListScriptOccurrenceStringOk)
			if !ok || len(scriptPage.Value) != int(tc.count) {
				t.Fatalf("script page = %+v", scriptPage)
			}
			call, ok := requireInvocationResult(api.CallByID(tc.ctx, string(calls[0]))).(bind.ResultOptionCallStringOk)
			if !ok || call.Value.IsSome != (tc.count != 0) {
				t.Fatalf("call lookup = %+v", call)
			}
			script, ok := requireInvocationResult(api.ScriptByID(tc.ctx, string(scripts[0]))).(bind.ResultOptionScriptOccurrenceStringOk)
			if !ok || script.Value.IsSome != (tc.count != 0) {
				t.Fatalf("script lookup = %+v", script)
			}
		})
	}
	// Reusing the transport implementation must not retain the previous graph.
	ctx := context.Background()
	for _, result := range []any{
		requireInvocationResult(api.Describe(ctx)),
		requireInvocationResult(api.Calls(ctx, 0, 1)),
		requireInvocationResult(api.Scripts(ctx, 0, 1)),
		requireInvocationResult(api.CallByID(ctx, string(calls[0]))),
		requireInvocationResult(api.ScriptByID(ctx, string(scripts[0]))),
	} {
		requireInvocationError(t, result, "invocation graph is unavailable")
	}
}
