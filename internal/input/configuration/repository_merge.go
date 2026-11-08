package config

import (
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
	configtree "actionlint.kjanat.dev/v2/internal/input/configuration/tree"
)

func loadConfigTree(path string, read func(string) ([]byte, error), normalize func(string, document.Document) (document.Document, error)) (configtree.Loaded, error) {
	return configtree.LoadWithMerge(path, read, normalize, MergeDocuments)
}

// MergeDocuments applies inheritance rules, including atomic repository facts.
func MergeDocuments(base, overlay any) any {
	merged := configtree.Merge(base, overlay)
	if selected := field(overlay, "repository"); selected != nil && isMapping(merged) {
		next := cloneMapping(merged)
		setField(next, "repository", mergeRepositoryNodes(field(base, "repository"), selected))
		return next
	}
	return merged
}

func mergeRepositoryNodes(base, overlay any) any {
	if !isMapping(overlay) {
		return rawValue(overlay)
	}
	next := cloneMapping(base)
	if base != nil && isNull(base) {
		next = mapping()
		for _, key := range []string{"default-workflow-permissions", "runners", "variables", "secrets", "environments"} {
			setField(next, key, nil)
		}
	}
	if !isMapping(next) {
		return overlay
	}
	for key, value := range members(overlay) {
		setField(next, key, cloneValue(value))
	}
	return next
}
