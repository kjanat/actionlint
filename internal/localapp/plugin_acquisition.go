package localapp

import (
	"slices"

	"actionlint.kjanat.dev/v2/internal/acquisitionhttp"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifact"
)

// explicitPluginSources is an invocation boundary, separate from constructing
// sources from repository declarations or lock evidence.
func explicitPluginSources(paths []string) (PluginSources, error) {
	sources := PluginSources{Paths: slices.Clone(paths), grants: make(map[string]acquisitionhttp.Grant, len(paths))}
	for _, source := range paths {
		grant, err := pluginartifact.ExplicitSource(source)
		if err != nil {
			return PluginSources{}, err
		}
		sources.grants[source] = grant
	}
	return sources, nil
}

func (s PluginSources) artifactOptions(source string) pluginartifact.Options {
	options := artifactOptions()
	options.Network = s.grants[source]
	return options
}

func (p *PluginProject) artifactOptions() pluginartifact.Options {
	options := artifactOptions()
	options.Network = p.StatusOptions.Network
	return options
}
