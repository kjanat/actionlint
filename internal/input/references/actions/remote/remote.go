// Package remote shares provider metadata within one analysis request.
package remote

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"

	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/input/references/remotecontent"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	factavailability "actionlint.kjanat.dev/v2/internal/model/availability"
	repofacts "actionlint.kjanat.dev/v2/internal/model/repository"
)

type (
	repositoryKey struct {
		host string
		id   uint64
	}
	location struct{ host, name string }
)

// Service separates mutable-reference lookups from immutable manifest parsing.
// It is owned by one analysis request and contains no process-global data.
type Service struct {
	provider actions.Provider
	store    *remotecontent.Store[actions.Reference, ActionInspection, actionmeta.DefinitionInspection]
	// mu guards the repository names and IDs seen during this request.
	mu         sync.Mutex
	names      map[repositoryKey]string
	identities map[location]repofacts.ID
}

func New(provider actions.Provider) *Service {
	return &Service{provider: provider, store: remotecontent.New[actions.Reference, ActionInspection](actionmeta.InspectDefinition), names: make(map[repositoryKey]string), identities: make(map[location]repofacts.ID)}
}

// ActionInspection keeps validated call-site facts beside their complete availability.
type ActionInspection struct {
	manifest     actions.Manifest
	definition   actionmeta.Definition
	availability actions.MetadataAvailability
}

func (r ActionInspection) Definition() actionmeta.Definition          { return r.definition }
func (r ActionInspection) Availability() actions.MetadataAvailability { return r.availability }
func (r ActionInspection) Manifest() (actions.Manifest, bool)         { return r.manifest.Value() }

// Lookup validates references even when metadata fetching is disabled. An absent
// definition leaves only metadata-dependent checks unavailable.
func (s *Service) Lookup(ctx context.Context, spec string) (actionmeta.Definition, error) {
	result, err := s.Inspect(ctx, spec)
	return result.definition, err
}

// Inspect retains unavailable reasons and invalid manifest diagnostics.
func (s *Service) Inspect(ctx context.Context, spec string) (ActionInspection, error) {
	if err := ctx.Err(); err != nil {
		return ActionInspection{}, err
	}
	reference, err := actions.ParseReference("github.com", spec)
	if err != nil {
		// Syntax rules own malformed references; no metadata lookup is needed.
		// Cancellation still belongs to this analysis, even on that path.
		return ActionInspection{}, ctx.Err()
	}
	if s == nil {
		return ActionInspection{availability: actions.MissingMetadata(factavailability.LookupDisabled())}, nil
	}
	return s.store.Acquire(ctx, reference, func(ctx context.Context) (ActionInspection, error) {
		return s.fetch(ctx, reference)
	})
}

func (s *Service) fetch(ctx context.Context, reference actions.Reference) (ActionInspection, error) {
	if s.provider == nil {
		return ActionInspection{availability: actions.MissingMetadata(factavailability.LookupDisabled())}, nil
	}
	result, err := s.provider.Lookup(ctx, reference)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return ActionInspection{}, context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return ActionInspection{}, context.DeadlineExceeded
		}
		// Provider failures are metadata unavailability. Cancellation of the
		// analysis remains fatal, including cancellation during that lookup.
		return ActionInspection{availability: actions.MissingMetadata(factavailability.Failed())}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return ActionInspection{}, err
	}
	if missing, unavailable := result.Unavailability(); unavailable {
		return ActionInspection{availability: actions.MissingMetadata(missing)}, nil
	}
	manifest := result
	identity := manifest.Identity()
	if identity.Commit() == "" || identity.Host() != reference.Host() || !strings.EqualFold(identity.Repository(), reference.Repository()) || identity.Directory() != reference.Directory() {
		return ActionInspection{}, errors.New("remote action provider returned a foreign manifest identity")
	}
	if manifest.Selection().Request() != reference {
		return ActionInspection{}, errors.New("remote action provider returned a selection for another request")
	}
	if err := s.admitRepository(identity); err != nil {
		return ActionInspection{}, err
	}
	inspection, unchanged := s.store.Parse(remotecontent.Content{
		Location: remotecontent.Location{Host: identity.Host(), Repository: identity.Repository(), Commit: identity.Commit(), Path: identity.Directory()},
		Name:     manifest.Filename(),
		Bytes:    manifest.Content(),
	})
	if !unchanged {
		return ActionInspection{}, errors.New("remote action provider changed content for one immutable identity")
	}
	return inspected(manifest, inspection), nil
}

// admitRepository rejects a repository whose name or known ID changed during analysis.
func (s *Service) admitRepository(identity actions.Identity) error {
	id, known := identity.RepositoryID().Value()
	if !known {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	name := location{host: identity.Host(), name: strings.ToLower(identity.Repository())}
	repository := repositoryKey{host: identity.Host(), id: id}
	if previous, exists := s.names[repository]; exists && !strings.EqualFold(previous, identity.Repository()) {
		return errors.New("remote action repository name changed during analysis")
	}
	if previous, exists := s.identities[name]; exists && previous.Conflicts(identity.RepositoryID()) {
		return errors.New("remote action repository identity changed during analysis")
	}
	s.names[repository], s.identities[name] = identity.Repository(), identity.RepositoryID()
	return nil
}

func inspected(manifest actions.Manifest, inspection actionmeta.DefinitionInspection) ActionInspection {
	if definition, valid := inspection.Definition(); valid {
		return ActionInspection{manifest: manifest, definition: allowReviewedAdditionalInputs(manifest, definition), availability: actions.AvailableMetadata(manifest)}
	}
	var problems []actionmeta.MetadataProblem
	manifestPath := manifest.Filename()
	if manifest.Identity().Directory() != "" {
		manifestPath = manifest.Identity().Directory() + "/" + manifestPath
	}
	for _, problem := range inspection.Problems() {
		problem.Path = manifestPath
		problems = append(problems, problem)
	}
	return ActionInspection{manifest: manifest, availability: actions.InvalidMetadata(manifest, problems)}
}

func (s *Service) Coverage() []actions.Coverage {
	if s == nil {
		return nil
	}
	results := s.store.Results()
	coverage := make([]actions.Coverage, 0, len(results))
	for ref, result := range results {
		name := ref.Repository()
		if ref.Directory() != "" {
			name += "/" + ref.Directory()
		}
		coverage = append(coverage, actions.Coverage{Reference: name + "@" + ref.Ref(), Availability: result.availability})
	}
	slices.SortFunc(coverage, func(a, b actions.Coverage) int { return strings.Compare(a.Reference, b.Reference) })
	return coverage
}
