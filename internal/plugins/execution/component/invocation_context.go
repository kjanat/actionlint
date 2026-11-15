package component

import (
	"context"
	"errors"
	"fmt"
	"math"

	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	bind "actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint/invocationcontext"
)

const InvocationContextInterface = bind.InterfaceName

// MaxInvocationPageSize bounds one calls or scripts page returned to a guest.
const MaxInvocationPageSize = 256

type (
	invocationGraphKey struct{}
	invocationContext  struct{}
)

// Generated bindings reserve Go errors for transport traps. Query failures
// belong to the WIT result, so these adapters never return a Go error.
func (invocationContext) Describe(ctx context.Context) (bind.ResultSummaryString, error) {
	return describeInvocation(ctx), nil
}

func (invocationContext) Calls(ctx context.Context, offset, limit uint32) (bind.ResultListCallString, error) {
	return invocationCalls(ctx, offset, limit), nil
}

func (invocationContext) Scripts(ctx context.Context, offset, limit uint32) (bind.ResultListScriptOccurrenceString, error) {
	return invocationScripts(ctx, offset, limit), nil
}

func (invocationContext) CallByID(ctx context.Context, id string) (bind.ResultOptionCallString, error) {
	return invocationCallByID(ctx, id), nil
}

func (invocationContext) ScriptByID(ctx context.Context, id string) (bind.ResultOptionScriptOccurrenceString, error) {
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

func describeInvocation(ctx context.Context) bind.ResultSummaryString {
	graph, err := invocationGraph(ctx)
	if err != nil {
		return bind.ResultSummaryStringErr{Value: err.Error()}
	}
	entry, budget := graph.Entry(), graph.Budget()
	kind := bind.EntryKindWorkflow
	if entry.Kind == invocation.EntryAction {
		kind = bind.EntryKindAction
	}
	value := bind.Summary{
		Entry:  bind.Entry{ID: string(graph.RootID()), Kind: kind, Reference: invocationReference(entry.Reference)},
		Budget: bind.Budget{MaxDepth: uint32(budget.MaxDepth), MaxCalls: uint32(budget.MaxCalls), MaxScripts: uint32(budget.MaxScripts)},
		Calls:  uint32(graph.CallCount()), Scripts: uint32(graph.ScriptCount()), Complete: !graph.Incomplete(),
	}
	if stop, present := graph.Stop(); present {
		limit := bind.StopLimitTotal
		if stop.Limit == invocation.LimitScripts {
			limit = bind.StopLimitScripts
		}
		value.Stop = bind.SomeStop(bind.Stop{Parent: string(stop.Parent), Site: invocationReference(stop.Site), Limit: limit, Max: uint32(stop.Max)})
	}
	if interrupted, present := graph.EntryInterruption(); present {
		value.EntryInterruption = bind.SomeInterruption(invocationInterruption(interrupted))
	}
	return bind.ResultSummaryStringOk{Value: value}
}

// invocationPage validates guest page bounds and returns the page end.
func invocationPage(offset, limit uint32, length int) (int, bool) {
	if uint64(offset) > uint64(length) || limit == 0 || limit > MaxInvocationPageSize {
		return 0, false
	}
	return int(offset) + min(int(limit), length-int(offset)), true
}

func invocationCalls(ctx context.Context, offset, limit uint32) bind.ResultListCallString {
	graph, err := invocationGraph(ctx)
	if err != nil {
		return bind.ResultListCallStringErr{Value: err.Error()}
	}
	end, ok := invocationPage(offset, limit, graph.CallCount())
	if !ok {
		return bind.ResultListCallStringErr{Value: "invalid invocation page bounds"}
	}
	calls := graph.CallRange(int(offset), end)
	values := make([]bind.Call, 0, len(calls))
	for _, call := range calls {
		value, err := invocationCall(graph, call)
		if err != nil {
			return bind.ResultListCallStringErr{Value: err.Error()}
		}
		values = append(values, value)
	}
	return bind.ResultListCallStringOk{Value: values}
}

func invocationScripts(ctx context.Context, offset, limit uint32) bind.ResultListScriptOccurrenceString {
	graph, err := invocationGraph(ctx)
	if err != nil {
		return bind.ResultListScriptOccurrenceStringErr{Value: err.Error()}
	}
	end, ok := invocationPage(offset, limit, graph.ScriptCount())
	if !ok {
		return bind.ResultListScriptOccurrenceStringErr{Value: "invalid invocation page bounds"}
	}
	scripts := graph.ScriptRange(int(offset), end)
	values := make([]bind.ScriptOccurrence, 0, len(scripts))
	for _, script := range scripts {
		values = append(values, invocationScript(graph, script))
	}
	return bind.ResultListScriptOccurrenceStringOk{Value: values}
}

func invocationCallByID(ctx context.Context, id string) bind.ResultOptionCallString {
	graph, err := invocationGraph(ctx)
	if err != nil {
		return bind.ResultOptionCallStringErr{Value: err.Error()}
	}
	call, found := graph.Call(invocation.ID(id))
	if !found {
		return bind.ResultOptionCallStringOk{Value: bind.NoneCall()}
	}
	value, err := invocationCall(graph, call)
	if err != nil {
		return bind.ResultOptionCallStringErr{Value: err.Error()}
	}
	return bind.ResultOptionCallStringOk{Value: bind.SomeCall(value)}
}

func invocationScriptByID(ctx context.Context, id string) bind.ResultOptionScriptOccurrenceString {
	graph, err := invocationGraph(ctx)
	if err != nil {
		return bind.ResultOptionScriptOccurrenceStringErr{Value: err.Error()}
	}
	script, found := graph.Script(invocation.ID(id))
	if !found {
		return bind.ResultOptionScriptOccurrenceStringOk{Value: bind.NoneScriptOccurrence()}
	}
	return bind.ResultOptionScriptOccurrenceStringOk{Value: bind.SomeScriptOccurrence(invocationScript(graph, script))}
}

func invocationReference(ref source.Reference) bind.SourceReference {
	return bind.SourceReference{Document: ref.Document, Revision: ref.Revision, Node: ref.Node, Address: ref.Address}
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

func invocationScript(graph invocation.Graph, script invocation.Script) bind.ScriptOccurrence {
	kind := bind.ScriptKindRun
	if script.Kind == invocation.ScriptActionInput {
		kind = bind.ScriptKindActionInput
	}
	depth, _ := graph.Depth(script.Owner)
	return bind.ScriptOccurrence{
		ID: string(script.ID), Owner: string(script.Owner), Ancestry: invocationAncestry(graph, script.Owner),
		Depth: uint32(depth), Declaration: invocationReference(script.Declaration), Kind: kind, Input: script.Input,
	}
}

func invocationInterruption(outcome invocation.Outcome) bind.Interruption {
	reason := bind.InterruptionReasonCanceled
	if outcome.Reason() == invocation.ReasonOperational {
		reason = bind.InterruptionReasonOperationalFailure
	}
	return bind.Interruption{Reason: reason, Detail: outcome.Detail()}
}

var (
	unavailableReasons = map[invocation.Reason]bind.UnavailableReason{
		invocation.ReasonNonLocal: bind.UnavailableReasonNonLocal, invocation.ReasonNotFetched: bind.UnavailableReasonNotFetched,
		invocation.ReasonLookupDisabled: bind.UnavailableReasonLookupDisabled, invocation.ReasonPlacementUncertain: bind.UnavailableReasonPlacementUncertain,
	}
	unresolvedReasons = map[invocation.Reason]bind.UnresolvedReason{
		invocation.ReasonInvalidCall: bind.UnresolvedReasonInvalidCall, invocation.ReasonMissingManifest: bind.UnresolvedReasonMissingManifest,
		invocation.ReasonReadFailed: bind.UnresolvedReasonReadFailed, invocation.ReasonParseFailed: bind.UnresolvedReasonParseFailed, invocation.ReasonMappingFailed: bind.UnresolvedReasonMappingFailed,
	}
)

func invocationCall(graph invocation.Graph, call invocation.Call) (bind.Call, error) {
	facts, _ := call.Facts()
	if call.Depth() < 0 || uint64(call.Depth()) > math.MaxUint32 || facts.Step.Line < 0 || facts.Step.Column < 0 || uint64(facts.Step.Line) > math.MaxUint32 || uint64(facts.Step.Column) > math.MaxUint32 {
		return bind.Call{}, fmt.Errorf("call %s position exceeds WIT coordinate range", call.ID())
	}
	value := bind.Call{
		ID: string(call.ID()), Parent: string(call.Parent()), Ancestry: invocationAncestry(graph, call.Parent()), Depth: uint32(call.Depth()),
		Site: invocationReference(call.Site()),
	}
	if facts.Step.Line != 0 {
		value.Step = bind.SomePosition(bind.Position{Line: uint32(facts.Step.Line), Column: uint32(facts.Step.Column)})
	}
	if facts.HasUses {
		value.Uses = bind.SomeString(facts.Uses)
	}
	switch facts.Condition {
	case invocation.ConditionEnabled:
		value.Condition = bind.ConditionEnabled
	case invocation.ConditionSkipped:
		value.Condition = bind.ConditionSkipped
	case invocation.ConditionRuntimeDependent:
		value.Condition = bind.ConditionRuntimeDependent
	case invocation.ConditionInvalid:
	}
	outcome := call.Outcome
	target, hasTarget := outcome.Target()
	switch outcome.Kind() {
	case invocation.OutcomeExpanded:
		value.Outcome = bind.OutcomeExpanded{Value: invocationReference(target)}
	case invocation.OutcomeOpaque:
		reason := bind.OpaqueReasonDockerfileOnly
		if outcome.Reason() == invocation.ReasonNonComposite {
			reason = bind.OpaqueReasonNonComposite
		}
		value.Outcome = bind.OutcomeOpaque{Value: bind.Opaque{Reason: reason, Detail: outcome.Detail()}}
	case invocation.OutcomeUnavailable:
		value.Outcome = bind.OutcomeUnavailable{Value: bind.Unavailable{Reason: unavailableReasons[outcome.Reason()], Detail: outcome.Detail()}}
	case invocation.OutcomeUnresolved:
		value.Outcome = bind.OutcomeUnresolved{Value: bind.Unresolved{Reason: unresolvedReasons[outcome.Reason()], Detail: outcome.Detail()}}
	case invocation.OutcomeDynamic:
		value.Outcome = bind.OutcomeDynamic{Value: outcome.Detail()}
	case invocation.OutcomeCycle:
		cycle, _ := outcome.Cycle()
		value.Outcome = bind.OutcomeCycle{Value: bind.Cycle{Target: invocationReference(target), Active: invocationReference(cycle.Active), Owner: string(cycle.Owner), Closing: string(cycle.Closing)}}
	case invocation.OutcomeDepthLimit:
		_, maximum, _ := outcome.Limit()
		value.Outcome = bind.OutcomeDepthLimited{Value: uint32(maximum)}
	case invocation.OutcomeInterrupted:
		interrupted := bind.Interrupted{Interruption: invocationInterruption(outcome)}
		if hasTarget {
			interrupted.Target = bind.SomeSourceReference(invocationReference(target))
		}
		value.Outcome = bind.OutcomeInterrupted{Value: interrupted}
	case invocation.OutcomeInvalid:
		return bind.Call{}, fmt.Errorf("invalid call outcome %s", outcome.Kind())
	}
	return value, nil
}
