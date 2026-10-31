package config

import (
	configtree "actionlint.kjanat.dev/v2/internal/config/tree"
	"github.com/kjanat/go-yaml/v4"
)

func loadConfigTree(path string, read func(string) ([]byte, error), normalize func(string, *yaml.Node) (*yaml.Node, error)) (configtree.Loaded, error) {
	return configtree.LoadWithMerge(path, read, normalize, MergeDocuments)
}

// MergeDocuments applies the configuration document's inheritance rules before
// plugin discovery or analysis, including atomic repository facts.
func MergeDocuments(base, overlay *yaml.Node, inputs map[*yaml.Node]Input) *yaml.Node {
	merged := configtree.Merge(base, overlay, inputs)
	if selected := field(overlay, "repository"); selected != nil && merged.Kind == yaml.MappingNode {
		next := cloneMapping(merged)
		inputs[next] = inputs[merged]
		setField(next, "repository", mergeRepositoryNodes(field(base, "repository"), selected, inputs))
		return next
	}
	return merged
}

// Each repository field is a complete fact. Inheriting fields is useful;
// merging two available name sets would invent a fact neither source supplied.
func mergeRepositoryNodes(base, overlay *yaml.Node, inputs map[*yaml.Node]Input) *yaml.Node {
	if overlay.Kind != yaml.MappingNode {
		return overlay
	}
	next := cloneMapping(base)
	if base != nil && base.ShortTag() == "!!null" {
		next = mapping()
		reset := *base
		for _, key := range []string{"default-workflow-permissions", "runners", "variables", "secrets", "environments"} {
			setField(next, key, &reset)
		}
	}
	if next.Kind != yaml.MappingNode {
		return overlay
	}
	for i := 0; i < len(overlay.Content); i += 2 {
		setField(next, overlay.Content[i].Value, overlay.Content[i+1])
	}
	inputs[next] = inputs[overlay]
	return next
}
