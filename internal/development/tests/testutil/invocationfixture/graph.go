// Package invocationfixture builds published invocation graphs for tests.
package invocationfixture

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
)

// Workflow is the entry declaration of every fixture graph.
var Workflow = sourcetext.Reference{Document: "workflow", Revision: "r", Node: "document"}

// Action is the body the fixture call expands into.
var Action = sourcetext.Reference{Document: "action", Revision: "r", Node: "document"}

// Script declares one occurrence. Entry scripts belong to Workflow, others to
// the call's Action body. A non-empty Input makes it an action-input script.
type Script struct {
	Node, Input string
	Entry       bool
}

// Graph publishes a workflow entry with one enabled call expanded into Action.
// Entry scripts are registered first, then call scripts, each in order.
func Graph(tb testing.TB, scripts ...Script) invocation.Graph {
	tb.Helper()
	builder, err := invocation.NewBuilder(invocation.EntryWorkflow, Workflow, invocation.Budget{MaxDepth: 1, MaxCalls: 1, MaxScripts: max(1, len(scripts))})
	if err != nil {
		tb.Fatal(err)
	}
	add := func(owner invocation.Context, body sourcetext.Reference, entry bool) {
		for _, script := range scripts {
			if script.Entry != entry {
				continue
			}
			kind := invocation.ScriptRun
			if script.Input != "" {
				kind = invocation.ScriptActionInput
			}
			declaration := sourcetext.Reference{Document: body.Document, Revision: body.Revision, Node: script.Node}
			if _, err := builder.AddScript(owner, declaration, kind, script.Input); err != nil {
				tb.Fatal(err)
			}
		}
	}
	add(builder.RootContext(), Workflow, true)
	site := sourcetext.Reference{Document: Workflow.Document, Revision: Workflow.Revision, Node: "call"}
	ticket, err := builder.Begin(builder.RootContext(), site, invocation.Facts{Condition: invocation.ConditionEnabled, Uses: "./action", HasUses: true})
	if err != nil {
		tb.Fatal(err)
	}
	call, admitted := ticket.Context()
	if !admitted {
		tb.Fatalf("fixture call was not admitted: %v", ticket.Status())
	}
	expanded, err := invocation.Expanded(Action)
	if err != nil {
		tb.Fatal(err)
	}
	if _, err := builder.Resolve(call, expanded); err != nil {
		tb.Fatal(err)
	}
	add(call, Action, false)
	if err := builder.Close(call); err != nil {
		tb.Fatal(err)
	}
	graph, err := builder.Publish()
	if err != nil {
		tb.Fatal(err)
	}
	return graph
}
