// Package lookup reads action definitions from workspace and repository metadata.
package lookup

import (
	"context"
	"errors"
	"strings"

	actionremote "actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	actionlocal "actionlint.kjanat.dev/v2/internal/model/actions/local"
	"actionlint.kjanat.dev/v2/internal/model/calls/references"
)

// Resolver reads workspace metadata and already-acquired repository metadata.
// It projects both sources into the same immutable action definition.
type Resolver struct {
	ctx    context.Context
	local  actionlocal.MetadataLookup
	remote *actionremote.ResolvedActions
}

// New binds action-definition lookup to one request and workspace execution scope.
// A nil metadata source leaves its references unresolved.
func New(ctx context.Context, local actionlocal.MetadataLookup, remote *actionremote.ResolvedActions) *Resolver {
	return &Resolver{ctx: ctx, local: local, remote: remote}
}

// Lookup returns an absent definition when its metadata is unavailable.
// Resolution diagnostics and source provenance belong to the metadata sources.
// Repository lookups never start a network request.
func (r *Resolver) Lookup(spec string) (actionmeta.Definition, error) {
	if err := r.ctx.Err(); err != nil {
		return actionmeta.Definition{}, err
	}
	localSpec := spec
	if normalized, ok := references.SelfRepositoryLocal(spec); ok {
		localSpec = normalized
	}
	if !strings.HasPrefix(localSpec, "./") {
		return r.remote.Lookup(spec), nil
	}
	if r.local == nil {
		return actionmeta.Definition{}, nil
	}
	metadata, _, err := r.local.FindMetadata(spec)
	if _, unresolved := errors.AsType[actionlocal.UnresolvedError](err); unresolved {
		return actionmeta.Definition{}, nil
	}
	if _, mapping := errors.AsType[actionlocal.MappingError](err); mapping {
		return actionmeta.Definition{}, nil
	}
	if err != nil {
		return actionmeta.Definition{}, err
	}
	return actionmeta.NewDefinition(metadata), nil
}
