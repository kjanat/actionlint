package remote

import (
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/availability"
)

// ResolvedActions reads acquired metadata without starting provider requests.
type ResolvedActions struct {
	service *Service
}

func (s *Service) Resolved() *ResolvedActions { return &ResolvedActions{service: s} }

// Inspect treats a nil reader as disabled remote metadata lookup.
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

func (r *ResolvedActions) Lookup(spec string) actionmeta.Definition {
	return r.Inspect(spec).Definition()
}
