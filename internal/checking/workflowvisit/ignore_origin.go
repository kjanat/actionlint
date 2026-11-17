package workflowvisit

import (
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

// VisitWithOrigin annotates findings produced by a synchronous pass visit.
// Asynchronous producers snapshot their own origins before scheduling work.
func VisitWithOrigin(pass Pass, origin *workflows.IgnoreOrigin, hasIgnores bool, visit func() error) error {
	rule, ok := pass.(Rule)
	if !ok {
		return visit()
	}
	before := rule.FindingCount()
	defer rule.AnnotateSince(before, func(item *finding.Finding) {
		if item.Owner == nil && item.Source.Content == nil {
			item.Owner = &finding.Owner{Job: origin.Job, Step: origin.StepPosition}
		}
	})
	// Metadata can have file-specific ignores even when its caller has none.
	_, actionRule := rule.(MetadataParticipant)
	if !actionRule && !hasIgnores {
		return visit()
	}
	if participant, ok := pass.(originParticipant); ok {
		participant.SetIgnoreOrigin(origin)
		return visit()
	}
	start := rule.FindingCount()
	err := visit()
	rule.AnnotateSince(start, func(item *finding.Finding) {
		if !hasIgnores && (item.MetadataIdentity == nil || item.Source.Content == nil) {
			return
		}
		if item.Source.Content == nil && item.IgnoreOrigin == nil {
			item.IgnoreOrigin = origin
		} else if item.Source.Content != nil && item.IgnoreOrigin != nil {
			// Each invocation owns its origin; shared metadata never stores caller state.
			inner := *item.IgnoreOrigin
			inner.Job = origin.Job
			item.IgnoreOrigin = &inner
		}
	})
	return err
}

// originParticipant snapshots context for asynchronous diagnostic producers.
type originParticipant interface {
	SetIgnoreOrigin(*workflows.IgnoreOrigin)
}

// HasConfiguredIgnores reports whether cfg configures ignore patterns.
func HasConfiguredIgnores(cfg *configuration.Config) bool { return cfg != nil && len(cfg.Ignores) != 0 }
