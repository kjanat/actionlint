package engine

import (
	"actionlint.kjanat.dev/internal/builtin"
	repoconfig "actionlint.kjanat.dev/internal/config"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
)

// Asynchronous analyzers snapshot the origin before scheduling their work.
// Synchronous passes can safely annotate only the findings produced by this visit.
func visitWithIgnoreOrigin(pass workflowvisit.Pass, origin *findingmodel.IgnoreOrigin, hasIgnores bool, visit func() error) error {
	rule, ok := pass.(workflowvisit.Rule)
	if !ok {
		return visit()
	}
	before := len(rule.Errs())
	defer func() {
		for _, finding := range rule.Errs()[before:] {
			if finding.Owner == nil && finding.Source == nil {
				finding.Owner = &findingmodel.Owner{Job: origin.Job, Step: origin.StepPosition}
			}
		}
	}()
	// Metadata can have file-specific ignores even when its caller has none.
	_, actionRule := rule.(builtin.MetadataParticipant)
	if !actionRule && !hasIgnores {
		return visit()
	}
	if participant, ok := pass.(originParticipant); ok {
		participant.setIgnoreOrigin(origin)
		return visit()
	}

	start := len(rule.Errs())
	err := visit()
	for _, finding := range rule.Errs()[start:] {
		if !hasIgnores && (finding.MetadataIdentity == nil || finding.Source == nil) {
			continue
		}
		if finding.Source == nil && finding.IgnoreOrigin == nil {
			finding.IgnoreOrigin = origin
		} else if finding.Source != nil && finding.IgnoreOrigin != nil {
			// Each invocation owns its origin; shared metadata never stores caller state.
			inner := *finding.IgnoreOrigin
			inner.Job = origin.Job
			finding.IgnoreOrigin = &inner
		}
	}
	return err
}

// originParticipant snapshots context for asynchronous diagnostic producers.
type originParticipant interface {
	setIgnoreOrigin(*findingmodel.IgnoreOrigin)
}

func hasConfiguredIgnores(cfg *repoconfig.Config) bool { return cfg != nil && len(cfg.Ignores) != 0 }
