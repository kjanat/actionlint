package references

import (
	"context"
)

type repositoryBoundResolver struct {
	provider Resolver
	access   RepositoryAccess
}

func BindRepositoryAccess(provider Resolver, access RepositoryAccess) Resolver {
	return repositoryBoundResolver{provider: provider, access: access}
}

func (r repositoryBoundResolver) Resolve(ctx context.Context, request Request) (ResolutionRecord, error) {
	return r.provider.Resolve(WithRepositoryAccess(ctx, r.access), request)
}

func (r repositoryBoundResolver) NewSession() Resolver {
	if factory, ok := r.provider.(SessionFactory); ok {
		r.provider = factory.NewSession()
	}
	return r
}
