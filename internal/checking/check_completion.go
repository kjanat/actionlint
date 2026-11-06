package checking

import (
	"cmp"
	"slices"
	"sync"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

// checkCompletion records incomplete source analyses independently of suppression
// usage. Reports and suppression cleanup consume the same detached records.
type checkCompletion struct {
	mu          sync.Mutex
	incomplete  map[checkRevision]bool
	unavailable map[checkRule]string
}
type (
	checkRevision struct{ document, revision string }
	checkRule     struct{ document, revision, rule string }
)

func (u *checkCompletion) markIncomplete(ref sourcepos.Reference) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.incomplete == nil {
		u.incomplete = make(map[checkRevision]bool)
	}
	u.incomplete[checkRevision{ref.Document, ref.Revision}] = true
}

func (u *checkCompletion) markUnavailable(ref sourcepos.Reference, rule, reason string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.unavailable == nil {
		u.unavailable = make(map[checkRule]string)
	}
	u.unavailable[checkRule{ref.Document, ref.Revision, rule}] = reason
}

func (u *checkCompletion) incompleteChecks() []report.IncompleteCheck {
	u.mu.Lock()
	defer u.mu.Unlock()
	checks := make([]report.IncompleteCheck, 0, len(u.incomplete)+len(u.unavailable))
	for revision := range u.incomplete {
		checks = append(checks, report.IncompleteCheck{Source: sourcepos.Reference{Document: revision.document, Revision: revision.revision}, Reason: "source analysis did not complete"})
	}
	for rule, reason := range u.unavailable {
		checks = append(checks, report.IncompleteCheck{Source: sourcepos.Reference{Document: rule.document, Revision: rule.revision}, Rule: rule.rule, Reason: reason})
	}
	slices.SortFunc(checks, func(a, b report.IncompleteCheck) int {
		return cmp.Or(cmp.Compare(a.Source.Document, b.Source.Document), cmp.Compare(a.Source.Revision, b.Source.Revision), cmp.Compare(a.Rule, b.Rule))
	})
	return checks
}
