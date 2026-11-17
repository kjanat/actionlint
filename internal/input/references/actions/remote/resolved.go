package remote

import (
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/availability"
)

// ResolvedActions reads acquired metadata without starting provider requests.
// Source preparation can add results; readers cannot change or fetch them.
type ResolvedActions struct {
	service *Service
}

// Resolved returns a read-only view of metadata acquired for this request.
func (s *Service) Resolved() *ResolvedActions { return &ResolvedActions{service: s} }

// Inspect returns not-fetched when source preparation has not acquired a reference.
// A nil reader means remote metadata lookup is disabled.
func (r *ResolvedActions) Inspect(spec string) ActionInspection {
	if r == nil || r.service == nil {
		return ActionInspection{availability: actions.MissingMetadata(availability.LookupDisabled())}
	}
	reference, err := actions.ParseReference("github.com", spec)
	if err != nil {
		return ActionInspection{}
	}
	inspection, _ := r.service.store.Lookup(reference)
	return inspection
}

// Lookup returns an absent definition when metadata is unavailable or invalid.
// Inspect also reports the reason and any manifest problems.
func (r *ResolvedActions) Lookup(spec string) actionmeta.Definition {
	return r.Inspect(spec).Definition()
}
