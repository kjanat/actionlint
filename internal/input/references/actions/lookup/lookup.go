// Package lookup reads action definitions from workspace and repository metadata.
package lookup

import (
	"context"
	"errors"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/references/actions/localactions"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/calls/callreferences"
)

type Resolver struct {
	ctx    context.Context
	local  localactions.ActionMetadataLookup
	remote *remote.ResolvedActions
}

// New leaves references unresolved for a nil metadata source.
func New(ctx context.Context, local localactions.ActionMetadataLookup, remote *remote.ResolvedActions) *Resolver {
	return &Resolver{ctx: ctx, local: local, remote: remote}
}

// Lookup starts no network request for repository references.
func (r *Resolver) Lookup(spec string) (actionmeta.Definition, error) {
	if err := r.ctx.Err(); err != nil {
		return actionmeta.Definition{}, err
	}
	localSpec := spec
	if normalized, ok := callreferences.SelfRepositoryLocal(spec); ok {
		localSpec = normalized
	}
	if !strings.HasPrefix(localSpec, "./") {
		return r.remote.Lookup(spec), nil
	}
	if r.local == nil {
		return actionmeta.Definition{}, nil
	}
	metadata, _, err := r.local.FindMetadata(spec)
	if _, unresolved := errors.AsType[localactions.UnresolvedError](err); unresolved {
		return actionmeta.Definition{}, nil
	}
	if _, mapping := errors.AsType[localactions.MappingError](err); mapping {
		return actionmeta.Definition{}, nil
	}
	if err != nil {
		return actionmeta.Definition{}, err
	}
	return actionmeta.NewDefinition(metadata), nil
}
