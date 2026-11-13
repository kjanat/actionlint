package github

import (
	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
)

func (g *GitHub) ResolverEnabled() bool { return g.mode == Online }

// WithResolver uses the analysis session's resolver for action and workflow lookups.
func (g *GitHub) WithResolver(resolver references.Resolver) actions.Provider {
	configured := *g
	configured.resolver = func() references.Resolver { return resolver }
	configured.listings = newRefListings()
	return &configured
}
