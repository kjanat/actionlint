package execution

import (
	"context"
	"time"

	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/plugins/bundled/commitish"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/component"
)

func (l *Loaded) ReferenceResolvers() []references.Resolver {
	var resolvers []references.Resolver
	for _, plugin := range l.Components {
		if plugin.HasReferenceResolver() {
			resolvers = append(resolvers, plugin)
		}
	}
	return resolvers
}

// LoadDefaultResolver loads Commitish without downloading another executable.
func (l *Loaded) LoadDefaultResolver(ctx context.Context) (references.Resolver, error) {
	plugin, err := component.LoadArtifact(ctx, commitish.Artifact(), component.Options{Timeout: 30 * time.Second})
	if err != nil {
		return nil, err
	}
	l.Components = append(l.Components, plugin)
	return plugin, nil
}
