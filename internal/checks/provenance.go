package checks

import (
	"actionlint.kjanat.dev/v2/internal/builtin"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

// VisitWithOrigin annotates findings produced by a synchronous pass visit.
// Asynchronous producers snapshot their own origins before scheduling work.
func VisitWithOrigin(pass workflowvisit.Pass, origin *findingmodel.IgnoreOrigin, hasIgnores bool, visit func() error) error {
	rule, ok := pass.(workflowvisit.Rule)
	if !ok {
		return visit()
	}
	before := rule.FindingCount()
	defer rule.AnnotateSince(before, func(finding *findingmodel.Finding) {
		if finding.Owner == nil && finding.Source == nil {
			finding.Owner = &findingmodel.Owner{Job: origin.Job, Step: origin.StepPosition}
		}
	})
	// Metadata can have file-specific ignores even when its caller has none.
	_, actionRule := rule.(builtin.MetadataParticipant)
	if !actionRule && !hasIgnores {
		return visit()
	}
	if participant, ok := pass.(originParticipant); ok {
		participant.SetIgnoreOrigin(origin)
		return visit()
	}

	start := rule.FindingCount()
	err := visit()
	rule.AnnotateSince(start, func(finding *findingmodel.Finding) {
		if !hasIgnores && (finding.MetadataIdentity == nil || finding.Source == nil) {
			return
		}
		if finding.Source == nil && finding.IgnoreOrigin == nil {
			finding.IgnoreOrigin = origin
		} else if finding.Source != nil && finding.IgnoreOrigin != nil {
			// Each invocation owns its origin; shared metadata never stores caller state.
			inner := *finding.IgnoreOrigin
			inner.Job = origin.Job
			finding.IgnoreOrigin = &inner
		}
	})
	return err
}

// originParticipant snapshots context for asynchronous diagnostic producers.
type originParticipant interface {
	SetIgnoreOrigin(*findingmodel.IgnoreOrigin)
}

func HasConfiguredIgnores(cfg *repoconfig.Config) bool { return cfg != nil && len(cfg.Ignores) != 0 }
