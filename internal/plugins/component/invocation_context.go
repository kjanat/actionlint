package component

import (
	"context"
	"errors"
	"fmt"
	"math"

	"actionlint.kjanat.dev/v2/analyzer"
	bind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/invocationcontext"
	"actionlint.kjanat.dev/v2/source"
)

const InvocationContextInterface = bind.InterfaceName

type invocationGraphKey struct{}
type invocationContext struct{}

func invocationGraph(ctx context.Context) (analyzer.CallGraph, error) {
	if err := ctx.Err(); err != nil {
		return analyzer.CallGraph{}, err
	}
	graph, _ := ctx.Value(invocationGraphKey{}).(analyzer.CallGraph)
	if !graph.Valid() {
		return graph, errors.New("invocation graph is unavailable in this call")
	}
	budget := graph.Budget()
	for _, n := range []int{budget.MaxDepth, budget.MaxCalls, budget.MaxScripts, graph.CallCount(), graph.ScriptCount()} {
		if n < 0 || uint64(n) > math.MaxUint32 {
			return analyzer.CallGraph{}, errors.New("invocation graph exceeds WIT integer range")
		}
	}
	return graph, nil
}

func (invocationContext) Describe(ctx context.Context) (bind.ResultSummaryString, error) {
	graph, err := invocationGraph(ctx)
	if err != nil {
		return bind.ResultSummaryStringErr{Value: err.Error()}, nil
	}
	entry, budget := graph.Entry(), graph.Budget()
	kind := bind.EntryKindWorkflow
	if entry.Kind == analyzer.ActionEntry {
		kind = bind.EntryKindAction
	}
	value := bind.Summary{
		Entry:  bind.Entry{ID: string(entry.ID), Kind: kind, Reference: invocationReference(entry.Reference)},
		Budget: bind.Budget{MaxDepth: uint32(budget.MaxDepth), MaxCalls: uint32(budget.MaxCalls), MaxScripts: uint32(budget.MaxScripts)},
		Calls:  uint32(graph.CallCount()), Scripts: uint32(graph.ScriptCount()), Complete: graph.Complete(),
	}
	if stop, present := graph.Stop().Get(); present {
		limit := bind.StopLimitTotal
		if stop.Limit == analyzer.ScriptLimit {
			limit = bind.StopLimitScripts
		}
		value.Stop = bind.SomeStop(bind.Stop{Parent: string(stop.Parent), Site: invocationReference(stop.Site), Limit: limit, Max: uint32(stop.Max)})
	}
	if interrupted, present := graph.EntryInterruption().Get(); present {
		value.EntryInterruption = bind.SomeInterruption(invocationInterruption(interrupted))
	}
	return bind.ResultSummaryStringOk{Value: value}, nil
}

func (invocationContext) Calls(ctx context.Context, offset, limit uint32) (bind.ResultListCallString, error) {
	graph, err := invocationGraph(ctx)
	if err != nil {
		return bind.ResultListCallStringErr{Value: err.Error()}, nil
	}
	if uint64(offset) > uint64(graph.CallCount()) || limit == 0 || limit > analyzer.MaxCallGraphPageSize {
		return bind.ResultListCallStringErr{Value: "invalid invocation page bounds"}, nil
	}
	calls, err := graph.CallsPage(int(offset), int(limit))
	if err != nil {
		return bind.ResultListCallStringErr{Value: err.Error()}, nil
	}
	values := make([]bind.Call, 0, len(calls))
	for _, call := range calls {
		value, err := invocationCall(call)
		if err != nil {
			return bind.ResultListCallStringErr{Value: err.Error()}, nil
		}
		values = append(values, value)
	}
	return bind.ResultListCallStringOk{Value: values}, nil
}

func (invocationContext) Scripts(ctx context.Context, offset, limit uint32) (bind.ResultListScriptOccurrenceString, error) {
	graph, err := invocationGraph(ctx)
	if err != nil {
		return bind.ResultListScriptOccurrenceStringErr{Value: err.Error()}, nil
	}
	if uint64(offset) > uint64(graph.ScriptCount()) || limit == 0 || limit > analyzer.MaxCallGraphPageSize {
		return bind.ResultListScriptOccurrenceStringErr{Value: "invalid invocation page bounds"}, nil
	}
	scripts, err := graph.ScriptsPage(int(offset), int(limit))
	if err != nil {
		return bind.ResultListScriptOccurrenceStringErr{Value: err.Error()}, nil
	}
	values := make([]bind.ScriptOccurrence, 0, len(scripts))
	for _, script := range scripts {
		values = append(values, invocationScript(script))
	}
	return bind.ResultListScriptOccurrenceStringOk{Value: values}, nil
}

func (invocationContext) CallByID(ctx context.Context, id string) (bind.ResultOptionCallString, error) {
	graph, err := invocationGraph(ctx)
	if err != nil {
		return bind.ResultOptionCallStringErr{Value: err.Error()}, nil
	}
	call, found := graph.Call(analyzer.OccurrenceID(id))
	if !found {
		return bind.ResultOptionCallStringOk{Value: bind.NoneCall()}, nil
	}
	value, err := invocationCall(call)
	if err != nil {
		return bind.ResultOptionCallStringErr{Value: err.Error()}, nil
	}
	return bind.ResultOptionCallStringOk{Value: bind.SomeCall(value)}, nil
}

func (invocationContext) ScriptByID(ctx context.Context, id string) (bind.ResultOptionScriptOccurrenceString, error) {
	graph, err := invocationGraph(ctx)
	if err != nil {
		return bind.ResultOptionScriptOccurrenceStringErr{Value: err.Error()}, nil
	}
	script, found := graph.Script(analyzer.OccurrenceID(id))
	if !found {
		return bind.ResultOptionScriptOccurrenceStringOk{Value: bind.NoneScriptOccurrence()}, nil
	}
	return bind.ResultOptionScriptOccurrenceStringOk{Value: bind.SomeScriptOccurrence(invocationScript(script))}, nil
}

func invocationReference(ref source.Reference) bind.SourceReference {
	return bind.SourceReference{Document: ref.Document, Revision: ref.Revision, Node: ref.Node, Address: ref.Address}
}

func invocationAncestry(ids []analyzer.OccurrenceID) []string {
	values := make([]string, len(ids))
	for i, id := range ids {
		values[i] = string(id)
	}
	return values
}

func invocationScript(script analyzer.ScriptOccurrence) bind.ScriptOccurrence {
	kind := bind.ScriptKindRun
	if script.Kind == analyzer.ActionInputScript {
		kind = bind.ScriptKindActionInput
	}
	return bind.ScriptOccurrence{
		ID: string(script.ID), Owner: string(script.Owner), Ancestry: invocationAncestry(script.Ancestry),
		Depth: uint32(script.Depth), Declaration: invocationReference(script.Declaration), Kind: kind, Input: script.Input,
	}
}

func invocationInterruption(value analyzer.Interruption) bind.Interruption {
	reason := bind.InterruptionReasonCanceled
	if value.Reason == analyzer.OperationalFailure {
		reason = bind.InterruptionReasonOperationalFailure
	}
	return bind.Interruption{Reason: reason, Detail: value.Detail}
}

func invocationCall(call analyzer.Call) (bind.Call, error) {
	if call.Depth < 0 || uint64(call.Depth) > math.MaxUint32 || call.Step.Line < 0 || call.Step.Column < 0 || uint64(call.Step.Line) > math.MaxUint32 || uint64(call.Step.Column) > math.MaxUint32 {
		return bind.Call{}, fmt.Errorf("call %s position exceeds WIT coordinate range", call.ID)
	}
	value := bind.Call{
		ID: string(call.ID), Parent: string(call.Parent), Ancestry: invocationAncestry(call.Ancestry), Depth: uint32(call.Depth),
		Site: invocationReference(call.Site),
	}
	if call.Step.Line != 0 {
		value.Step = bind.SomePosition(bind.Position{Line: uint32(call.Step.Line), Column: uint32(call.Step.Column)})
	}
	if call.HasUses {
		value.Uses = bind.SomeString(call.Uses)
	}
	switch call.Condition {
	case analyzer.CallEnabled:
		value.Condition = bind.ConditionEnabled
	case analyzer.CallSkipped:
		value.Condition = bind.ConditionSkipped
	case analyzer.CallRuntimeDependent:
		value.Condition = bind.ConditionRuntimeDependent
	}
	switch outcome := call.Outcome.Value().(type) {
	case analyzer.ExpandedCall:
		value.Outcome = bind.OutcomeExpanded{Value: invocationReference(outcome.Target)}
	case analyzer.OpaqueCall:
		reason := bind.OpaqueReasonDockerfileOnly
		if outcome.Reason == analyzer.NonComposite {
			reason = bind.OpaqueReasonNonComposite
		}
		value.Outcome = bind.OutcomeOpaque{Value: bind.Opaque{Reason: reason, Detail: outcome.Detail}}
	case analyzer.UnavailableCall:
		reasons := map[analyzer.UnavailableReason]bind.UnavailableReason{
			analyzer.NonLocal: bind.UnavailableReasonNonLocal, analyzer.NotFetched: bind.UnavailableReasonNotFetched,
			analyzer.LookupDisabled: bind.UnavailableReasonLookupDisabled, analyzer.PlacementUncertain: bind.UnavailableReasonPlacementUncertain,
		}
		value.Outcome = bind.OutcomeUnavailable{Value: bind.Unavailable{Reason: reasons[outcome.Reason], Detail: outcome.Detail}}
	case analyzer.UnresolvedCall:
		reasons := map[analyzer.UnresolvedReason]bind.UnresolvedReason{
			analyzer.InvalidCall: bind.UnresolvedReasonInvalidCall, analyzer.MissingManifest: bind.UnresolvedReasonMissingManifest,
			analyzer.ReadFailed: bind.UnresolvedReasonReadFailed, analyzer.ParseFailed: bind.UnresolvedReasonParseFailed, analyzer.MappingFailed: bind.UnresolvedReasonMappingFailed,
		}
		value.Outcome = bind.OutcomeUnresolved{Value: bind.Unresolved{Reason: reasons[outcome.Reason], Detail: outcome.Detail}}
	case analyzer.DynamicCall:
		value.Outcome = bind.OutcomeDynamic{Value: outcome.Detail}
	case analyzer.CycleCall:
		value.Outcome = bind.OutcomeCycle{Value: bind.Cycle{Target: invocationReference(outcome.Target), Active: invocationReference(outcome.Active), Owner: string(outcome.Owner), Closing: string(outcome.Closing)}}
	case analyzer.DepthLimitedCall:
		value.Outcome = bind.OutcomeDepthLimited{Value: uint32(outcome.Max)}
	case analyzer.InterruptedCall:
		interrupted := bind.Interrupted{Interruption: invocationInterruption(outcome.Interruption)}
		if target, present := outcome.Target.Get(); present {
			interrupted.Target = bind.SomeSourceReference(invocationReference(target))
		}
		value.Outcome = bind.OutcomeInterrupted{Value: interrupted}
	default:
		return bind.Call{}, fmt.Errorf("invalid call outcome %T", outcome)
	}
	return value, nil
}
