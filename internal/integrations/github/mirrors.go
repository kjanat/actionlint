package github

import (
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/input/references/workflows"
	"github.com/bmatcuk/doublestar/v4"
)

func (g *GitHub) WithMetadataMirrors(access references.MirrorAccess, excludedRepositories []string) actions.Provider {
	configured := *g
	configured.mirrors = access
	configured.excludedMirrorRepositories = slices.Clone(excludedRepositories)
	return &configured
}

func (g *GitHub) WithWorkflowMetadataMirrors(access references.MirrorAccess, excludedRepositories []string) workflows.Provider {
	configured := *g
	configured.mirrors = access
	configured.excludedMirrorRepositories = slices.Clone(excludedRepositories)
	return &configured
}

func (l *refListings) observeVisibility(repository string, private *bool) {
	if private == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	key := strings.ToLower(repository)
	if !l.private[key] {
		l.private[key] = *private
	}
}

func (g *GitHub) mirrorAllowed(repository string) bool {
	if g.mirrors == references.NoMirrors {
		return false
	}
	for _, pattern := range g.excludedMirrorRepositories {
		if matched, err := doublestar.Match(strings.ToLower(pattern), "github.com/"+strings.ToLower(repository)); err != nil || matched {
			return false
		}
	}
	g.listings.mu.Lock()
	private, known := g.listings.private[strings.ToLower(repository)]
	g.listings.mu.Unlock()
	if known {
		return !private
	}
	return g.mirrors == references.AllowUnknownMirrors
}
