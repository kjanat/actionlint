// Package remote shares provider metadata within one analysis request.
package remote

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"

	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	factavailability "actionlint.kjanat.dev/v2/internal/model/availability"
	repofacts "actionlint.kjanat.dev/v2/internal/model/repository"
)

type repositoryKey struct {
	host string
	id   uint64
}
type (
	location   struct{ host, name string }
	contentKey struct {
		repository        location
		commit, directory string
	}
)

// Service separates mutable-reference lookups from immutable manifest parsing.
// It is owned by one analysis request and contains no process-global data.
type Service struct {
	provider   actions.Provider
	mu         sync.Mutex
	references map[actions.Reference]ActionInspection
	pending    map[actions.Reference]*pendingLookup
	manifests  map[contentKey]actionmeta.DefinitionInspection
	content    map[contentKey]string
	names      map[repositoryKey]string
	identities map[location]repofacts.ID
}

func New(provider actions.Provider) *Service {
	return &Service{provider: provider, references: make(map[actions.Reference]ActionInspection), pending: make(map[actions.Reference]*pendingLookup), manifests: make(map[contentKey]actionmeta.DefinitionInspection), content: make(map[contentKey]string), names: make(map[repositoryKey]string), identities: make(map[location]repofacts.ID)}
}

type pendingLookup struct {
	ready   chan struct{}
	result  ActionInspection
	failure lookupError
}

// Shared acquisition publishes immutable error data, never a retained error
// interface through which callers could reach request-owned state.
type lookupError struct {
	message                     string
	present, canceled, deadline bool
}

func failureOf(err error) lookupError {
	if err == nil {
		return lookupError{}
	}
	return lookupError{message: err.Error(), present: true, canceled: errors.Is(err, context.Canceled), deadline: errors.Is(err, context.DeadlineExceeded)}
}
func (f lookupError) Error() string { return f.message }
func (f lookupError) Is(target error) bool {
	return f.canceled && target == context.Canceled || f.deadline && target == context.DeadlineExceeded
}

func (f lookupError) err() error {
	if !f.present {
		return nil
	}
	return f
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
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return ActionInspection{}, err
	}
	if cached, ok := s.references[reference]; ok {
		s.mu.Unlock()
		return cached, nil
	}
	if pending, exists := s.pending[reference]; exists {
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return ActionInspection{}, ctx.Err()
		case <-pending.ready:
			if err := ctx.Err(); err != nil {
				return ActionInspection{}, err
			}
			return pending.result, pending.failure.err()
		}
	}
	pending := &pendingLookup{ready: make(chan struct{})}
	s.pending[reference] = pending
	s.mu.Unlock()
	metadata, err := s.fetch(ctx, reference)
	s.mu.Lock()
	if err == nil {
		s.references[reference] = metadata
	}
	pending.result, pending.failure = metadata, failureOf(err)
	delete(s.pending, reference)
	close(pending.ready)
	s.mu.Unlock()
	return metadata, pending.failure.err()
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
	s.mu.Lock()
	defer s.mu.Unlock()
	name := location{host: identity.Host(), name: strings.ToLower(identity.Repository())}
	if id, known := identity.RepositoryID().Value(); known {
		repository := repositoryKey{host: identity.Host(), id: id}
		if previous, exists := s.names[repository]; exists && !strings.EqualFold(previous, identity.Repository()) {
			return ActionInspection{}, errors.New("remote action repository name changed during analysis")
		}
		if previous, exists := s.identities[name]; exists && previous.Conflicts(identity.RepositoryID()) {
			return ActionInspection{}, errors.New("remote action repository identity changed during analysis")
		}
		s.names[repository], s.identities[name] = identity.Repository(), identity.RepositoryID()
	}
	key := contentKey{repository: name, commit: identity.Commit(), directory: identity.Directory()}
	if previous, exists := s.content[key]; exists {
		if previous != manifest.Filename()+"\x00"+manifest.Content() {
			return ActionInspection{}, errors.New("remote action provider changed content for one immutable identity")
		}
		return inspected(manifest, s.manifests[key]), nil
	}
	s.content[key] = manifest.Filename() + "\x00" + manifest.Content()
	inspection := actionmeta.InspectDefinition([]byte(manifest.Content()))
	s.manifests[key] = inspection
	return inspected(manifest, inspection), nil
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
	s.mu.Lock()
	defer s.mu.Unlock()
	coverage := make([]actions.Coverage, 0, len(s.references))
	for ref, result := range s.references {
		name := ref.Repository()
		if ref.Directory() != "" {
			name += "/" + ref.Directory()
		}
		coverage = append(coverage, actions.Coverage{Reference: name + "@" + ref.Ref(), Availability: result.availability})
	}
	slices.SortFunc(coverage, func(a, b actions.Coverage) int { return strings.Compare(a.Reference, b.Reference) })
	return coverage
}
