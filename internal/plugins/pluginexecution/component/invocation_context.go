package component

import (
	"context"
	"errors"
	"fmt"
	"math"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint/invocationcontext"
)

const InvocationContextInterface = invocationcontext.InterfaceName

const MaxInvocationPageSize = 256

type (
	invocationGraphKey struct{}
	invocationContext  struct{}
)

// Generated bindings reserve Go errors for transport traps. Query failures
// belong to the WIT result. These adapters never return a Go error.
func (invocationContext) Describe(ctx context.Context) (invocationcontext.ResultSummaryString, error) {
	return describeInvocation(ctx), nil
}

func (invocationContext) Calls(ctx context.Context, offset, limit uint32) (invocationcontext.ResultListCallString, error) {
	return invocationCalls(ctx, offset, limit), nil
}

func (invocationContext) Scripts(ctx context.Context, offset, limit uint32) (invocationcontext.ResultListScriptOccurrenceString, error) {
	return invocationScripts(ctx, offset, limit), nil
}

func (invocationContext) CallByID(ctx context.Context, id string) (invocationcontext.ResultOptionCallString, error) {
	return invocationCallByID(ctx, id), nil
}

func (invocationContext) ScriptByID(ctx context.Context, id string) (invocationcontext.ResultOptionScriptOccurrenceString, error) {
	return invocationScriptByID(ctx, id), nil
}

func invocationGraph(ctx context.Context) (invocation.Graph, error) {
	if err := ctx.Err(); err != nil {
		return invocation.Graph{}, err
	}
	graph, _ := ctx.Value(invocationGraphKey{}).(invocation.Graph)
	if !graph.Valid() {
		return graph, errors.New("invocation graph is unavailable in this call")
	}
	budget := graph.Budget()
	for _, n := range []int{budget.MaxDepth, budget.MaxCalls, budget.MaxScripts, graph.CallCount(), graph.ScriptCount()} {
		if n < 0 || uint64(n) > math.MaxUint32 {
			return invocation.Graph{}, errors.New("invocation graph exceeds WIT integer range")
		}
	}
	return graph, nil
}

func describeInvocation(ctx context.Context) invocationcontext.ResultSummaryString {
	graph, err := invocationGraph(ctx)
	if err != nil {
		return invocationcontext.ResultSummaryStringErr{Value: err.Error()}
	}
	entry, budget := graph.Entry(), graph.Budget()
	kind := invocationcontext.EntryKindWorkflow
	if entry.Kind == invocation.EntryAction {
		kind = invocationcontext.EntryKindAction
	}
	value := invocationcontext.Summary{
		Entry:  invocationcontext.Entry{ID: string(graph.RootID()), Kind: kind, Reference: invocationReference(entry.Reference)},
		Budget: invocationcontext.Budget{MaxDepth: uint32(budget.MaxDepth), MaxCalls: uint32(budget.MaxCalls), MaxScripts: uint32(budget.MaxScripts)},
		Calls:  uint32(graph.CallCount()), Scripts: uint32(graph.ScriptCount()), Complete: !graph.Incomplete(),
	}
	if stop, present := graph.Stop(); present {
		limit := invocationcontext.StopLimitTotal
		if stop.Limit == invocation.LimitScripts {
			limit = invocationcontext.StopLimitScripts
		}
		value.Stop = invocationcontext.SomeStop(invocationcontext.Stop{Parent: string(stop.Parent), Site: invocationReference(stop.Site), Limit: limit, Max: uint32(stop.Max)})
	}
	if interrupted, present := graph.EntryInterruption(); present {
		value.EntryInterruption = invocationcontext.SomeInterruption(invocationInterruption(interrupted))
	}
	return invocationcontext.ResultSummaryStringOk{Value: value}
}

func invocationPage(offset, limit uint32, length int) (int, bool) {
	if uint64(offset) > uint64(length) || limit == 0 || limit > MaxInvocationPageSize {
		return 0, false
	}
	return int(offset) + min(int(limit), length-int(offset)), true
}

func invocationCalls(ctx context.Context, offset, limit uint32) invocationcontext.ResultListCallString {
	graph, err := invocationGraph(ctx)
	if err != nil {
		return invocationcontext.ResultListCallStringErr{Value: err.Error()}
	}
	end, ok := invocationPage(offset, limit, graph.CallCount())
	if !ok {
		return invocationcontext.ResultListCallStringErr{Value: "invalid invocation page bounds"}
	}
	calls := graph.CallRange(int(offset), end)
	values := make([]invocationcontext.Call, 0, len(calls))
	for _, call := range calls {
		value, err := invocationCall(graph, call)
		if err != nil {
			return invocationcontext.ResultListCallStringErr{Value: err.Error()}
		}
		values = append(values, value)
	}
	return invocationcontext.ResultListCallStringOk{Value: values}
}

func invocationScripts(ctx context.Context, offset, limit uint32) invocationcontext.ResultListScriptOccurrenceString {
	graph, err := invocationGraph(ctx)
	if err != nil {
		return invocationcontext.ResultListScriptOccurrenceStringErr{Value: err.Error()}
	}
	end, ok := invocationPage(offset, limit, graph.ScriptCount())
	if !ok {
		return invocationcontext.ResultListScriptOccurrenceStringErr{Value: "invalid invocation page bounds"}
	}
	scripts := graph.ScriptRange(int(offset), end)
	values := make([]invocationcontext.ScriptOccurrence, 0, len(scripts))
	for _, script := range scripts {
		values = append(values, invocationScript(graph, script))
	}
	return invocationcontext.ResultListScriptOccurrenceStringOk{Value: values}
}

func invocationCallByID(ctx context.Context, id string) invocationcontext.ResultOptionCallString {
	graph, err := invocationGraph(ctx)
	if err != nil {
		return invocationcontext.ResultOptionCallStringErr{Value: err.Error()}
	}
	call, found := graph.Call(invocation.ID(id))
	if !found {
		return invocationcontext.ResultOptionCallStringOk{Value: invocationcontext.NoneCall()}
	}
	value, err := invocationCall(graph, call)
	if err != nil {
		return invocationcontext.ResultOptionCallStringErr{Value: err.Error()}
	}
	return invocationcontext.ResultOptionCallStringOk{Value: invocationcontext.SomeCall(value)}
}

func invocationScriptByID(ctx context.Context, id string) invocationcontext.ResultOptionScriptOccurrenceString {
	graph, err := invocationGraph(ctx)
	if err != nil {
		return invocationcontext.ResultOptionScriptOccurrenceStringErr{Value: err.Error()}
	}
	script, found := graph.Script(invocation.ID(id))
	if !found {
		return invocationcontext.ResultOptionScriptOccurrenceStringOk{Value: invocationcontext.NoneScriptOccurrence()}
	}
	return invocationcontext.ResultOptionScriptOccurrenceStringOk{Value: invocationcontext.SomeScriptOccurrence(invocationScript(graph, script))}
}

func invocationReference(ref sourcetext.Reference) invocationcontext.SourceReference {
	return invocationcontext.SourceReference{Document: ref.Document, Revision: ref.Revision, Node: ref.Node, Address: ref.Address}
}

// invocationAncestry lists the IDs from the entry through owner inclusive.
func invocationAncestry(graph invocation.Graph, owner invocation.ID) []string {
	chain, _ := graph.Chain(owner)
	values := make([]string, len(chain))
	for i, id := range chain {
		values[i] = string(id)
	}
	return values
}

func invocationScript(graph invocation.Graph, script invocation.Script) invocationcontext.ScriptOccurrence {
	kind := invocationcontext.ScriptKindRun
	if script.Kind == invocation.ScriptActionInput {
		kind = invocationcontext.ScriptKindActionInput
	}
	depth, _ := graph.Depth(script.Owner)
	return invocationcontext.ScriptOccurrence{
		ID: string(script.ID), Owner: string(script.Owner), Ancestry: invocationAncestry(graph, script.Owner),
		Depth: uint32(depth), Declaration: invocationReference(script.Declaration), Kind: kind, Input: script.Input,
	}
}

func invocationInterruption(outcome invocation.Outcome) invocationcontext.Interruption {
	reason := invocationcontext.InterruptionReasonCanceled
	if outcome.Reason() == invocation.ReasonOperational {
		reason = invocationcontext.InterruptionReasonOperationalFailure
	}
	return invocationcontext.Interruption{Reason: reason, Detail: outcome.Detail()}
}

var (
	unavailableReasons = map[invocation.Reason]invocationcontext.UnavailableReason{
		invocation.ReasonNonLocal: invocationcontext.UnavailableReasonNonLocal, invocation.ReasonNotFetched: invocationcontext.UnavailableReasonNotFetched,
		invocation.ReasonLookupDisabled: invocationcontext.UnavailableReasonLookupDisabled, invocation.ReasonPlacementUncertain: invocationcontext.UnavailableReasonPlacementUncertain,
	}
	unresolvedReasons = map[invocation.Reason]invocationcontext.UnresolvedReason{
		invocation.ReasonInvalidCall: invocationcontext.UnresolvedReasonInvalidCall, invocation.ReasonMissingManifest: invocationcontext.UnresolvedReasonMissingManifest,
		invocation.ReasonReadFailed: invocationcontext.UnresolvedReasonReadFailed, invocation.ReasonParseFailed: invocationcontext.UnresolvedReasonParseFailed, invocation.ReasonMappingFailed: invocationcontext.UnresolvedReasonMappingFailed,
	}
)

func invocationCall(graph invocation.Graph, call invocation.Call) (invocationcontext.Call, error) {
	facts, _ := call.Facts()
	if call.Depth() < 0 || uint64(call.Depth()) > math.MaxUint32 || facts.Step.Line < 0 || facts.Step.Column < 0 || uint64(facts.Step.Line) > math.MaxUint32 || uint64(facts.Step.Column) > math.MaxUint32 {
		return invocationcontext.Call{}, fmt.Errorf("call %s position exceeds WIT coordinate range", call.ID())
	}
	value := invocationcontext.Call{
		ID: string(call.ID()), Parent: string(call.Parent()), Ancestry: invocationAncestry(graph, call.Parent()), Depth: uint32(call.Depth()),
		Site: invocationReference(call.Site()),
	}
	if facts.Step.Line != 0 {
		value.Step = invocationcontext.SomePosition(invocationcontext.Position{Line: uint32(facts.Step.Line), Column: uint32(facts.Step.Column)})
	}
	if facts.HasUses {
		value.Uses = invocationcontext.SomeString(facts.Uses)
	}
	switch facts.Condition {
	case invocation.ConditionEnabled:
		value.Condition = invocationcontext.ConditionEnabled
	case invocation.ConditionSkipped:
		value.Condition = invocationcontext.ConditionSkipped
	case invocation.ConditionRuntimeDependent:
		value.Condition = invocationcontext.ConditionRuntimeDependent
	case invocation.ConditionInvalid:
	}
	outcome := call.Outcome
	target, hasTarget := outcome.Target()
	switch outcome.Kind() {
	case invocation.OutcomeExpanded:
		value.Outcome = invocationcontext.OutcomeExpanded{Value: invocationReference(target)}
	case invocation.OutcomeOpaque:
		reason := invocationcontext.OpaqueReasonDockerfileOnly
		if outcome.Reason() == invocation.ReasonNonComposite {
			reason = invocationcontext.OpaqueReasonNonComposite
		}
		value.Outcome = invocationcontext.OutcomeOpaque{Value: invocationcontext.Opaque{Reason: reason, Detail: outcome.Detail()}}
	case invocation.OutcomeUnavailable:
		value.Outcome = invocationcontext.OutcomeUnavailable{Value: invocationcontext.Unavailable{Reason: unavailableReasons[outcome.Reason()], Detail: outcome.Detail()}}
	case invocation.OutcomeUnresolved:
		value.Outcome = invocationcontext.OutcomeUnresolved{Value: invocationcontext.Unresolved{Reason: unresolvedReasons[outcome.Reason()], Detail: outcome.Detail()}}
	case invocation.OutcomeDynamic:
		value.Outcome = invocationcontext.OutcomeDynamic{Value: outcome.Detail()}
	case invocation.OutcomeCycle:
		cycle, _ := outcome.Cycle()
		value.Outcome = invocationcontext.OutcomeCycle{Value: invocationcontext.Cycle{Target: invocationReference(target), Active: invocationReference(cycle.Active), Owner: string(cycle.Owner), Closing: string(cycle.Closing)}}
	case invocation.OutcomeDepthLimit:
		_, maximum, _ := outcome.Limit()
		value.Outcome = invocationcontext.OutcomeDepthLimited{Value: uint32(maximum)}
	case invocation.OutcomeInterrupted:
		interrupted := invocationcontext.Interrupted{Interruption: invocationInterruption(outcome)}
		if hasTarget {
			interrupted.Target = invocationcontext.SomeSourceReference(invocationReference(target))
		}
		value.Outcome = invocationcontext.OutcomeInterrupted{Value: interrupted}
	case invocation.OutcomeInvalid:
		return invocationcontext.Call{}, fmt.Errorf("invalid call outcome %s", outcome.Kind())
	}
	return value, nil
}
