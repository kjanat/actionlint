package configuration

import "slices"

func containsNull(value any) bool {
	if isNull(value) {
		return true
	}
	for _, child := range members(value) {
		if containsNull(child) {
			return true
		}
	}
	return slices.ContainsFunc(elements(value), containsNull)
}

func hasUnknownRepositoryFacts(node any) bool {
	if repository := field(node, "repository"); repository != nil && containsNull(repository) {
		return true
	}
	if overrides := field(node, "overrides"); overrides != nil {
		return slices.ContainsFunc(elements(overrides), hasUnknownRepositoryFacts)
	}
	return false
}
