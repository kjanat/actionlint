package installation

import (
	"slices"

	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifacts"
)

// SourcesFromArguments copies plugin paths and grants network access to the
// endpoint of each explicitly supplied remote source.
func SourcesFromArguments(paths []string) (Sources, error) {
	sources := Sources{Paths: slices.Clone(paths), NetworkGrants: make(map[string]acquisitionhttp.Grant, len(paths))}
	for _, source := range paths {
		grant, err := pluginartifact.ExplicitSource(source)
		if err != nil {
			return Sources{}, err
		}
		sources.NetworkGrants[source] = grant
	}
	return sources, nil
}

// ArtifactOptions supplies cache settings and the grant attached to source.
func (s *Sources) ArtifactOptions(source string) pluginartifact.Options {
	options := DefaultArtifactOptions()
	options.Network = s.NetworkGrants[source]
	return options
}
