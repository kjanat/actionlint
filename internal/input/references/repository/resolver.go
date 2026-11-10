package repository

import (
	"context"

	"actionlint.kjanat.dev/v2/internal/input/references"
)

type resolver struct {
	provider references.Resolver
	access   Access
}

// Bind gives a resolver repository access for each of its calls.
func Bind(provider references.Resolver, access Access) references.Resolver {
	return resolver{provider: provider, access: access}
}

func (r resolver) Resolve(ctx context.Context, request references.Request) (references.Result, error) {
	return r.provider.Resolve(WithAccess(ctx, r.access), request)
}

func (r resolver) NewSession() references.Resolver {
	if factory, ok := r.provider.(references.SessionFactory); ok {
		r.provider = factory.NewSession()
	}
	return r
}
