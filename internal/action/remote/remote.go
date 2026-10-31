// Package remote shares provider metadata within one analysis request.
package remote

import (
	"context"
	"errors"
	"strings"
	"sync"

	"actionlint.kjanat.dev/v2/actions"
	actionmeta "actionlint.kjanat.dev/v2/internal/action"
)

type repositoryKey struct {
	host string
	id   uint64
}
type (
	location   struct{ host, name string }
	contentKey struct {
		repository        repositoryKey
		commit, directory string
	}
)

// Service separates mutable-reference lookups from immutable manifest parsing.
// It is owned by one analysis request and contains no process-global data.
type Service struct {
	provider   actions.Provider
	mu         sync.Mutex
	references map[actions.Reference]actionmeta.Definition
	manifests  map[contentKey]actionmeta.Definition
	content    map[contentKey]string
	names      map[repositoryKey]string
	identities map[location]uint64
}

func New(provider actions.Provider) *Service {
	return &Service{provider: provider, references: make(map[actions.Reference]actionmeta.Definition), manifests: make(map[contentKey]actionmeta.Definition), content: make(map[contentKey]string), names: make(map[repositoryKey]string), identities: make(map[location]uint64)}
}

// Lookup validates references even when metadata fetching is disabled. An absent
// definition leaves only metadata-dependent checks unavailable.
func (s *Service) Lookup(ctx context.Context, spec string) (actionmeta.Definition, error) {
	if err := ctx.Err(); err != nil {
		return actionmeta.Definition{}, err
	}
	reference, err := actions.ParseReference("github.com", spec)
	if err != nil {
		return actionmeta.Definition{}, nil
	}
	if s == nil || s.provider == nil {
		return actionmeta.Definition{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return actionmeta.Definition{}, err
	}
	if cached, ok := s.references[reference]; ok {
		return cached, nil
	}
	metadata, err := s.fetch(ctx, reference)
	if err == nil {
		s.references[reference] = metadata
	}
	return metadata, err
}

func (s *Service) fetch(ctx context.Context, reference actions.Reference) (actionmeta.Definition, error) {
	result, err := s.provider.Lookup(ctx, reference)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return actionmeta.Definition{}, context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return actionmeta.Definition{}, context.DeadlineExceeded
		}
		return actionmeta.Definition{}, nil
	}
	if err := ctx.Err(); err != nil {
		return actionmeta.Definition{}, err
	}
	manifest, available := result.Value()
	if !available {
		return actionmeta.Definition{}, nil
	}
	identity := manifest.Identity()
	if identity.Commit() == "" || identity.Host() != reference.Host() || !strings.EqualFold(identity.Repository(), reference.Repository()) || identity.Directory() != reference.Directory() {
		return actionmeta.Definition{}, errors.New("remote action provider returned a foreign manifest identity")
	}
	if manifest.Selection().Request() != reference {
		return actionmeta.Definition{}, errors.New("remote action provider returned a selection for another request")
	}
	repository := repositoryKey{host: identity.Host(), id: identity.RepositoryID()}
	name := location{host: identity.Host(), name: strings.ToLower(identity.Repository())}
	if previous, exists := s.names[repository]; exists && !strings.EqualFold(previous, identity.Repository()) {
		return actionmeta.Definition{}, errors.New("remote action repository name changed during analysis")
	}
	if previous, exists := s.identities[name]; exists && previous != identity.RepositoryID() {
		return actionmeta.Definition{}, errors.New("remote action repository identity changed during analysis")
	}
	s.names[repository], s.identities[name] = identity.Repository(), identity.RepositoryID()
	key := contentKey{repository: repository, commit: identity.Commit(), directory: identity.Directory()}
	if previous, exists := s.content[key]; exists {
		if previous != manifest.Content() {
			return actionmeta.Definition{}, errors.New("remote action provider changed content for one immutable identity")
		}
		return s.manifests[key], nil
	}
	s.content[key] = manifest.Content()
	definition := actionmeta.ParseDefinition([]byte(manifest.Content()))
	s.manifests[key] = definition
	return definition, nil
}
