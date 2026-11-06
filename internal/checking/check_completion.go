package checking

import (
	"cmp"
	"slices"
	"sync"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

type checkInvocationIndex map[checkRevision][]report.CheckInvocation

func indexCheckInvocations(graph invocation.Graph) checkInvocationIndex {
	if !graph.Has(graph.RootID()) {
		return nil
	}
	index := make(checkInvocationIndex)
	entry := graph.Entry().Reference
	add := func(ref sourcepos.Reference, id invocation.ID) {
		key := checkRevision{ref.Document, ref.Revision}
		index[key] = append(index[key], report.CheckInvocation{Entry: entry, ID: id})
	}
	add(entry, graph.RootID())
	for _, call := range graph.Calls() {
		if target, known := call.Outcome.Target(); known {
			add(target, call.ID())
		}
	}
	return index
}

// forSource returns every known call of this source revision. A source-only
// record is used when resolution stopped before the graph identified a target.
func (index checkInvocationIndex) forSource(ref sourcepos.Reference) []report.CheckInvocation {
	if contexts := index[checkRevision{ref.Document, ref.Revision}]; len(contexts) != 0 {
		return contexts
	}
	return []report.CheckInvocation{{}}
}

// checkCompletion records incomplete source analyses independently of suppression
// usage. Reports and suppression cleanup consume the same detached records.
type checkCompletion struct {
	mu          sync.Mutex
	incomplete  map[checkOccurrence]bool
	unavailable map[unavailableCheck]string
}
type (
	checkRevision   struct{ document, revision string }
	checkOccurrence struct {
		source     checkRevision
		invocation report.CheckInvocation
	}
	unavailableCheck struct {
		occurrence checkOccurrence
		rule       string
	}
)

func (u *checkCompletion) markIncomplete(ref sourcepos.Reference) {
	u.markIncompleteInvocation(ref, report.CheckInvocation{})
}

func (u *checkCompletion) markIncompleteInvocation(ref sourcepos.Reference, invocation report.CheckInvocation) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.incomplete == nil {
		u.incomplete = make(map[checkOccurrence]bool)
	}
	u.incomplete[checkOccurrence{checkRevision{ref.Document, ref.Revision}, invocation}] = true
}

func (u *checkCompletion) markUnavailable(ref sourcepos.Reference, rule, reason string) {
	u.markUnavailableInvocation(ref, report.CheckInvocation{}, rule, reason)
}

func (u *checkCompletion) markUnavailableInvocation(ref sourcepos.Reference, invocation report.CheckInvocation, rule, reason string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.unavailable == nil {
		u.unavailable = make(map[unavailableCheck]string)
	}
	u.unavailable[unavailableCheck{checkOccurrence{checkRevision{ref.Document, ref.Revision}, invocation}, rule}] = reason
}

func (u *checkCompletion) incompleteChecks() []report.IncompleteCheck {
	u.mu.Lock()
	defer u.mu.Unlock()
	checks := make([]report.IncompleteCheck, 0, len(u.incomplete)+len(u.unavailable))
	for occurrence := range u.incomplete {
		checks = append(checks, report.IncompleteCheck{Source: sourcepos.Reference{Document: occurrence.source.document, Revision: occurrence.source.revision}, Invocation: occurrence.invocation, Reason: "source analysis did not complete"})
	}
	for rule, reason := range u.unavailable {
		checks = append(checks, report.IncompleteCheck{Source: sourcepos.Reference{Document: rule.occurrence.source.document, Revision: rule.occurrence.source.revision}, Invocation: rule.occurrence.invocation, Rule: rule.rule, Reason: reason})
	}
	slices.SortFunc(checks, func(a, b report.IncompleteCheck) int {
		return cmp.Or(cmp.Compare(a.Source.Document, b.Source.Document), cmp.Compare(a.Source.Revision, b.Source.Revision),
			cmp.Compare(a.Invocation.Entry.Document, b.Invocation.Entry.Document), cmp.Compare(a.Invocation.Entry.Revision, b.Invocation.Entry.Revision),
			cmp.Compare(a.Invocation.Entry.Node, b.Invocation.Entry.Node), cmp.Compare(a.Invocation.Entry.Address, b.Invocation.Entry.Address),
			cmp.Compare(a.Invocation.ID, b.Invocation.ID), cmp.Compare(a.Rule, b.Rule))
	})
	return checks
}
