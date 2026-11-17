package configuration

import "slices"

// containsNull reports whether value or any value nested in it is null.
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

// hasUnknownRepositoryFacts finds null repository settings at the root or in overrides.
func hasUnknownRepositoryFacts(node any) bool {
	if repository := field(node, "repository"); repository != nil && containsNull(repository) {
		return true
	}
	if overrides := field(node, "overrides"); overrides != nil {
		return slices.ContainsFunc(elements(overrides), hasUnknownRepositoryFacts)
	}
	return false
}
