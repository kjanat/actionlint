// Package invocations projects semantic occurrences into check inputs.
package invocations

import (
	"fmt"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/results"
)

// Projection owns private records. Its accessors return detached values.
type Projection struct {
	graph checkinputs.CallGraph
}

func New(graph invocation.Graph) (Projection, error) {
	if !graph.Has(graph.RootID()) {
		return Projection{}, fmt.Errorf("%w: unpublished graph", checkinputs.ErrInvalidCallGraph)
	}
	entry, budget := graph.Entry(), graph.Budget()
	entryKind, err := projectEntryKind(entry.Kind)
	if err != nil {
		return Projection{}, err
	}
	view := checkinputs.CallGraphData{
		Entry:    checkinputs.InvocationEntry{ID: checkinputs.OccurrenceID(graph.RootID()), Kind: entryKind, Reference: entry.Reference},
		Budget:   checkinputs.InvocationBudget{MaxDepth: budget.MaxDepth, MaxCalls: budget.MaxCalls, MaxScripts: budget.MaxScripts},
		Complete: !graph.Incomplete(),
	}
	calls, scripts := graph.Calls(), graph.Scripts()
	view.Calls = make([]checkinputs.Call, len(calls))
	view.Scripts = make([]checkinputs.ScriptOccurrence, len(scripts))
	total := 0
	for _, call := range calls {
		total += call.Depth()
	}
	for _, script := range scripts {
		depth, exists := graph.Depth(script.Owner)
		if !exists {
			return Projection{}, fmt.Errorf("%w: unknown script owner", checkinputs.ErrInvalidCallGraph)
		}
		total += depth + 1
	}
	storage := make([]checkinputs.OccurrenceID, total)
	ancestry := func(parent []checkinputs.OccurrenceID, id invocation.ID) []checkinputs.OccurrenceID {
		length := len(parent) + 1
		path := storage[:length:length]
		storage = storage[length:]
		copy(path, parent)
		path[length-1] = checkinputs.OccurrenceID(id)
		return path
	}
	paths := map[invocation.ID][]checkinputs.OccurrenceID{graph.RootID(): nil}
	for i, call := range calls {
		facts, exists := call.Facts()
		if !exists {
			return Projection{}, fmt.Errorf("%w: call facts absent", checkinputs.ErrInvalidCallGraph)
		}
		condition, err := projectCondition(facts.Condition)
		if err != nil {
			return Projection{}, err
		}
		outcome, err := projectOutcome(call.Outcome)
		if err != nil {
			return Projection{}, err
		}
		parent, exists := paths[call.Parent()]
		if !exists {
			return Projection{}, fmt.Errorf("%w: parent not projected", checkinputs.ErrInvalidCallGraph)
		}
		path := ancestry(parent, call.Parent())
		view.Calls[i] = checkinputs.Call{
			ID: checkinputs.OccurrenceID(call.ID()), Parent: checkinputs.OccurrenceID(call.Parent()), Ancestry: path, Depth: call.Depth(), Site: call.Site(),
			Step: results.Position{Line: facts.Step.Line, Column: facts.Step.Column}, Uses: facts.Uses, HasUses: facts.HasUses, Condition: condition, Outcome: outcome,
		}
		paths[call.ID()] = path
	}
	for i, script := range scripts {
		kind, err := projectScriptKind(script.Kind)
		if err != nil {
			return Projection{}, err
		}
		parent, exists := paths[script.Owner]
		if !exists {
			return Projection{}, fmt.Errorf("%w: script owner not projected", checkinputs.ErrInvalidCallGraph)
		}
		depth, _ := graph.Depth(script.Owner)
		view.Scripts[i] = checkinputs.ScriptOccurrence{
			ID: checkinputs.OccurrenceID(script.ID), Owner: checkinputs.OccurrenceID(script.Owner), Ancestry: ancestry(parent, script.Owner), Depth: depth,
			Declaration: script.Declaration, Kind: kind, Input: script.Input,
		}
	}
	if stop, exists := graph.Stop(); exists {
		limit, err := projectLimit(stop.Limit)
		if err != nil {
			return Projection{}, err
		}
		view.Stop = checkinputs.Present(checkinputs.InvocationStop{Parent: checkinputs.OccurrenceID(stop.Parent), Site: stop.Site, Limit: limit, Max: stop.Max})
	}
	if interruption, exists := graph.EntryInterruption(); exists {
		reason, err := projectInterruptionReason(interruption.Reason())
		if err != nil {
			return Projection{}, err
		}
		view.EntryInterruption = checkinputs.Present(checkinputs.Interruption{Reason: reason, Detail: interruption.Detail()})
	}
	callGraph, err := checkinputs.NewCallGraph(view)
	if err != nil {
		return Projection{}, err
	}
	return Projection{graph: callGraph}, nil
}

func (projection Projection) Graph() checkinputs.CallGraph { return projection.graph }

func (projection Projection) Script(id invocation.ID) (checkinputs.ScriptOccurrence, bool) {
	return projection.graph.Script(checkinputs.OccurrenceID(id))
}

func projectCondition(condition invocation.Condition) (checkinputs.CallCondition, error) {
	switch condition {
	case invocation.ConditionEnabled:
		return checkinputs.CallEnabled, nil
	case invocation.ConditionSkipped:
		return checkinputs.CallSkipped, nil
	case invocation.ConditionRuntimeDependent:
		return checkinputs.CallRuntimeDependent, nil
	case invocation.ConditionInvalid:
		// Invalid conditions have no analyzer projection.
		fallthrough
	default:
		return "", fmt.Errorf("%w: unknown condition %d", checkinputs.ErrInvalidCallGraph, condition)
	}
}

func projectLimit(limit invocation.LimitKind) (checkinputs.StopLimit, error) {
	switch limit {
	case invocation.LimitTotal:
		return checkinputs.CallLimit, nil
	case invocation.LimitScripts:
		return checkinputs.ScriptLimit, nil
	case invocation.LimitInvalid, invocation.LimitDepth:
		// Depth stops are not analyzer stop limits.
		fallthrough
	default:
		return "", fmt.Errorf("%w: unknown stop limit %d", checkinputs.ErrInvalidCallGraph, limit)
	}
}

func projectOutcome(outcome invocation.Outcome) (checkinputs.CallOutcome, error) {
	target, hasTarget := outcome.Target()
	var value checkinputs.CallResolution
	switch outcome.Kind() {
	case invocation.OutcomeExpanded:
		value = checkinputs.ExpandedCall{Target: target}
	case invocation.OutcomeOpaque:
		reason, err := projectOpaqueReason(outcome.Reason())
		if err != nil {
			return checkinputs.CallOutcome{}, err
		}
		value = checkinputs.OpaqueCall{Reason: reason, Detail: outcome.Detail()}
	case invocation.OutcomeUnavailable:
		reason, err := projectUnavailableReason(outcome.Reason())
		if err != nil {
			return checkinputs.CallOutcome{}, err
		}
		value = checkinputs.UnavailableCall{Reason: reason, Detail: outcome.Detail()}
	case invocation.OutcomeUnresolved:
		reason, err := projectUnresolvedReason(outcome.Reason())
		if err != nil {
			return checkinputs.CallOutcome{}, err
		}
		value = checkinputs.UnresolvedCall{Reason: reason, Detail: outcome.Detail()}
	case invocation.OutcomeDynamic:
		value = checkinputs.DynamicCall{Detail: outcome.Detail()}
	case invocation.OutcomeCycle:
		cycle, _ := outcome.Cycle()
		value = checkinputs.CycleCall{Target: target, Active: cycle.Active, Owner: checkinputs.OccurrenceID(cycle.Owner), Closing: checkinputs.OccurrenceID(cycle.Closing)}
	case invocation.OutcomeDepthLimit:
		_, maximum, _ := outcome.Limit()
		value = checkinputs.DepthLimitedCall{Max: maximum}
	case invocation.OutcomeInterrupted:
		reason, err := projectInterruptionReason(outcome.Reason())
		if err != nil {
			return checkinputs.CallOutcome{}, err
		}
		interrupted := checkinputs.InterruptedCall{Reason: reason, Detail: outcome.Detail()}
		if hasTarget {
			interrupted.Target = checkinputs.Present(target)
		}
		value = interrupted
	case invocation.OutcomeInvalid:
		// Invalid outcomes have no analyzer projection.
		fallthrough
	default:
		return checkinputs.CallOutcome{}, fmt.Errorf("%w: unknown outcome %s", checkinputs.ErrInvalidCallGraph, outcome.Kind())
	}
	return checkinputs.NewCallOutcome(value)
}
