package remote

import (
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
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
func (r *ResolvedActions) Inspect(spec string) Result {
	if r == nil || r.service == nil {
		return Result{availability: actions.MissingMetadata(availability.LookupDisabled())}
	}
	reference, err := actions.ParseReference("github.com", spec)
	if err != nil {
		return Result{}
	}
	r.service.mu.Lock()
	defer r.service.mu.Unlock()
	return r.service.references[reference]
}

// Lookup returns an absent definition when metadata is unavailable or invalid.
// Inspect also reports the reason and any manifest problems.
func (r *ResolvedActions) Lookup(spec string) actionmeta.Definition {
	return r.Inspect(spec).Definition()
}
