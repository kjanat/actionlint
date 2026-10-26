package analyzer

import (
	"slices"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/source"
)

type graphOwner struct {
	depth    int
	ancestry []OccurrenceID
	body     source.Reference
	hasBody  bool
}

// Validate checks transport invariants without generating or resolving identities.
func (graph CallGraph) Validate() error {
	entry, budget := graph.Entry, graph.Budget
	if !validOccurrence(entry.ID) || !validSource(entry.Reference) || entry.Kind != WorkflowEntry && entry.Kind != ActionEntry {
		return invalidGraph("invalid entry")
	}
	if budget.MaxDepth <= 0 || budget.MaxCalls <= 0 || budget.MaxScripts <= 0 || len(graph.Calls) > budget.MaxCalls || len(graph.Scripts) > budget.MaxScripts {
		return invalidGraph("invalid or exceeded budget")
	}
	owners := map[OccurrenceID]graphOwner{entry.ID: {body: entry.Reference, hasBody: true}}
	ids := map[OccurrenceID]bool{entry.ID: true}
	complete, interruptions := true, 0
	for _, call := range graph.Calls {
		parent, found := owners[call.Parent]
		if !validOccurrence(call.ID) || ids[call.ID] || !found || !parent.hasBody {
			return invalidGraph("call %q has invalid identity or parent", call.ID)
		}
		if call.Depth != parent.depth+1 || !matchesAncestry(call.Ancestry, parent.ancestry, call.Parent) || !sourceIn(call.Site, parent.body) {
			return invalidGraph("call %q disagrees with parent ancestry or source", call.ID)
		}
		if call.Condition != CallEnabled && call.Condition != CallSkipped && call.Condition != CallRuntimeDependent || !utf8.ValidString(call.Uses) || !call.HasUses && call.Uses != "" {
			return invalidGraph("call %q has invalid facts", call.ID)
		}
		if call.Step.Line < 0 || call.Step.Column < 0 || (call.Step.Line == 0) != (call.Step.Column == 0) {
			return invalidGraph("call %q has invalid position", call.ID)
		}
		if _, err := NewCallOutcome(call.Outcome.Value()); err != nil {
			return err
		}
		owner := graphOwner{depth: call.Depth, ancestry: call.Ancestry}
		switch outcome := call.Outcome.Value().(type) {
		case ExpandedCall:
			owner.body, owner.hasBody = outcome.Target, true
		case OpaqueCall:
		case InterruptedCall:
			owner.body, owner.hasBody = outcome.Target.Get()
			complete = false
			interruptions++
		case CycleCall:
			active, exists := owners[outcome.Owner]
			if !exists || !active.hasBody || active.body != outcome.Active || !slices.Contains(call.Ancestry, outcome.Owner) || outcome.Closing != call.ID || outcome.Target.Document != outcome.Active.Document || outcome.Target.Node != outcome.Active.Node {
				return invalidGraph("call %q has invalid cycle provenance", call.ID)
			}
			complete = false
		case DepthLimitedCall:
			if outcome.Max != budget.MaxDepth || call.Depth != outcome.Max+1 {
				return invalidGraph("invalid depth frontier")
			}
			complete = false
		case UnavailableCall, UnresolvedCall, DynamicCall:
			complete = false
		default:
			return invalidGraph("unsupported outcome %T", outcome)
		}
		if _, limited := call.Outcome.Value().(DepthLimitedCall); !limited && call.Depth > budget.MaxDepth {
			return invalidGraph("call %q exceeds depth budget", call.ID)
		}
		owners[call.ID], ids[call.ID] = owner, true
	}
	for _, script := range graph.Scripts {
		owner, exists := owners[script.Owner]
		if !validOccurrence(script.ID) || ids[script.ID] || !exists || !owner.hasBody {
			return invalidGraph("script %q has invalid identity or owner", script.ID)
		}
		if script.Depth != owner.depth || !matchesAncestry(script.Ancestry, owner.ancestry, script.Owner) || !sourceIn(script.Declaration, owner.body) {
			return invalidGraph("script %q disagrees with owner ancestry or source", script.ID)
		}
		if script.Kind != RunScript && script.Kind != ActionInputScript || (script.Kind == RunScript) != (script.Input == "") || !utf8.ValidString(script.Input) {
			return invalidGraph("script %q has invalid kind or input", script.ID)
		}
		ids[script.ID] = true
	}
	if stop, exists := graph.Stop.Get(); exists {
		owner, found := owners[stop.Parent]
		if !found || !owner.hasBody || !sourceIn(stop.Site, owner.body) {
			return invalidGraph("invalid stop owner or source")
		}
		switch stop.Limit {
		case CallLimit:
			if stop.Max != budget.MaxCalls || len(graph.Calls) != stop.Max {
				return invalidGraph("invalid call stop")
			}
		case ScriptLimit:
			if stop.Max != budget.MaxScripts || len(graph.Scripts) != stop.Max {
				return invalidGraph("invalid script stop")
			}
		default:
			return invalidGraph("invalid stop limit %q", stop.Limit)
		}
		complete = false
	}
	if interruption, exists := graph.EntryInterruption.Get(); exists {
		if !validInterruption(interruption) {
			return invalidGraph("invalid entry interruption")
		}
		interruptions++
		complete = false
	}
	if interruptions > 1 || graph.Complete != complete {
		return invalidGraph("inconsistent completion or interruption state")
	}
	return nil
}

func matchesAncestry(path, parent []OccurrenceID, owner OccurrenceID) bool {
	return len(path) == len(parent)+1 && path[len(path)-1] == owner && slices.Equal(path[:len(parent)], parent)
}

func sourceIn(ref, body source.Reference) bool {
	return validSource(ref) && ref.Document == body.Document && ref.Revision == body.Revision
}
