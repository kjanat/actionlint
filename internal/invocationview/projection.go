// Package invocationview projects semantic occurrences into the analyzer contract.
package invocationview

import (
	"fmt"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
)

// Projection retains private records; accessors return detached values.
type Projection struct {
	graph analyzer.CallGraph
}

func New(graph invocation.Graph) (Projection, error) {
	if !graph.Has(graph.RootID()) {
		return Projection{}, fmt.Errorf("%w: unpublished graph", analyzer.ErrInvalidCallGraph)
	}
	entry, budget := graph.Entry(), graph.Budget()
	entryKind, err := projectEntryKind(entry.Kind)
	if err != nil {
		return Projection{}, err
	}
	view := analyzer.CallGraphData{
		Entry:    analyzer.InvocationEntry{ID: analyzer.OccurrenceID(graph.RootID()), Kind: entryKind, Reference: entry.Reference},
		Budget:   analyzer.InvocationBudget{MaxDepth: budget.MaxDepth, MaxCalls: budget.MaxCalls, MaxScripts: budget.MaxScripts},
		Complete: !graph.Incomplete(),
	}
	calls, scripts := graph.Calls(), graph.Scripts()
	view.Calls = make([]analyzer.Call, len(calls))
	view.Scripts = make([]analyzer.ScriptOccurrence, len(scripts))
	total := 0
	for _, call := range calls {
		total += call.Depth()
	}
	for _, script := range scripts {
		depth, exists := graph.Depth(script.Owner)
		if !exists {
			return Projection{}, fmt.Errorf("%w: unknown script owner", analyzer.ErrInvalidCallGraph)
		}
		total += depth + 1
	}
	storage := make([]analyzer.OccurrenceID, total)
	ancestry := func(parent []analyzer.OccurrenceID, id invocation.ID) []analyzer.OccurrenceID {
		length := len(parent) + 1
		path := storage[:length:length]
		storage = storage[length:]
		copy(path, parent)
		path[length-1] = analyzer.OccurrenceID(id)
		return path
	}
	paths := map[invocation.ID][]analyzer.OccurrenceID{graph.RootID(): nil}
	for i, call := range calls {
		facts, exists := call.Facts()
		if !exists {
			return Projection{}, fmt.Errorf("%w: call facts absent", analyzer.ErrInvalidCallGraph)
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
			return Projection{}, fmt.Errorf("%w: parent not projected", analyzer.ErrInvalidCallGraph)
		}
		path := ancestry(parent, call.Parent())
		view.Calls[i] = analyzer.Call{
			ID: analyzer.OccurrenceID(call.ID()), Parent: analyzer.OccurrenceID(call.Parent()), Ancestry: path, Depth: call.Depth(), Site: call.Site(),
			Step: diagnostic.Position{Line: facts.Step.Line, Column: facts.Step.Column}, Uses: facts.Uses, HasUses: facts.HasUses, Condition: condition, Outcome: outcome,
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
			return Projection{}, fmt.Errorf("%w: script owner not projected", analyzer.ErrInvalidCallGraph)
		}
		depth, _ := graph.Depth(script.Owner)
		view.Scripts[i] = analyzer.ScriptOccurrence{
			ID: analyzer.OccurrenceID(script.ID), Owner: analyzer.OccurrenceID(script.Owner), Ancestry: ancestry(parent, script.Owner), Depth: depth,
			Declaration: script.Declaration, Kind: kind, Input: script.Input,
		}
	}
	if stop, exists := graph.Stop(); exists {
		limit, err := projectLimit(stop.Limit)
		if err != nil {
			return Projection{}, err
		}
		view.Stop = analyzer.Present(analyzer.InvocationStop{Parent: analyzer.OccurrenceID(stop.Parent), Site: stop.Site, Limit: limit, Max: stop.Max})
	}
	if interruption, exists := graph.EntryInterruption(); exists {
		reason, err := projectInterruptionReason(interruption.Reason())
		if err != nil {
			return Projection{}, err
		}
		view.EntryInterruption = analyzer.Present(analyzer.Interruption{Reason: reason, Detail: interruption.Detail()})
	}
	retained, err := analyzer.NewCallGraph(view)
	if err != nil {
		return Projection{}, err
	}
	return Projection{graph: retained}, nil
}

func (projection Projection) Graph() analyzer.CallGraph { return projection.graph }

func (projection Projection) Script(id invocation.ID) (analyzer.ScriptOccurrence, bool) {
	return projection.graph.Script(analyzer.OccurrenceID(id))
}

func projectCondition(condition invocation.Condition) (analyzer.CallCondition, error) {
	switch condition {
	case invocation.ConditionEnabled:
		return analyzer.CallEnabled, nil
	case invocation.ConditionSkipped:
		return analyzer.CallSkipped, nil
	case invocation.ConditionRuntimeDependent:
		return analyzer.CallRuntimeDependent, nil
	default:
		return "", fmt.Errorf("%w: unknown condition %d", analyzer.ErrInvalidCallGraph, condition)
	}
}

func projectLimit(limit invocation.LimitKind) (analyzer.StopLimit, error) {
	switch limit {
	case invocation.LimitTotal:
		return analyzer.CallLimit, nil
	case invocation.LimitScripts:
		return analyzer.ScriptLimit, nil
	default:
		return "", fmt.Errorf("%w: unknown stop limit %d", analyzer.ErrInvalidCallGraph, limit)
	}
}

func projectOutcome(outcome invocation.Outcome) (analyzer.CallOutcome, error) {
	target, hasTarget := outcome.Target()
	var value analyzer.CallResolution
	switch outcome.Kind() {
	case invocation.OutcomeExpanded:
		value = analyzer.ExpandedCall{Target: target}
	case invocation.OutcomeOpaque:
		reason, err := projectOpaqueReason(outcome.Reason())
		if err != nil {
			return analyzer.CallOutcome{}, err
		}
		value = analyzer.OpaqueCall{Reason: reason, Detail: outcome.Detail()}
	case invocation.OutcomeUnavailable:
		reason, err := projectUnavailableReason(outcome.Reason())
		if err != nil {
			return analyzer.CallOutcome{}, err
		}
		value = analyzer.UnavailableCall{Reason: reason, Detail: outcome.Detail()}
	case invocation.OutcomeUnresolved:
		reason, err := projectUnresolvedReason(outcome.Reason())
		if err != nil {
			return analyzer.CallOutcome{}, err
		}
		value = analyzer.UnresolvedCall{Reason: reason, Detail: outcome.Detail()}
	case invocation.OutcomeDynamic:
		value = analyzer.DynamicCall{Detail: outcome.Detail()}
	case invocation.OutcomeCycle:
		cycle, _ := outcome.Cycle()
		value = analyzer.CycleCall{Target: target, Active: cycle.Active, Owner: analyzer.OccurrenceID(cycle.Owner), Closing: analyzer.OccurrenceID(cycle.Closing)}
	case invocation.OutcomeDepthLimit:
		_, maximum, _ := outcome.Limit()
		value = analyzer.DepthLimitedCall{Max: maximum}
	case invocation.OutcomeInterrupted:
		reason, err := projectInterruptionReason(outcome.Reason())
		if err != nil {
			return analyzer.CallOutcome{}, err
		}
		interrupted := analyzer.InterruptedCall{Reason: reason, Detail: outcome.Detail()}
		if hasTarget {
			interrupted.Target = analyzer.Present(target)
		}
		value = interrupted
	default:
		return analyzer.CallOutcome{}, fmt.Errorf("%w: unknown outcome %s", analyzer.ErrInvalidCallGraph, outcome.Kind())
	}
	return analyzer.NewCallOutcome(value)
}
