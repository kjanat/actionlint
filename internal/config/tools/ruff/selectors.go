package ruff

import "slices"

// SupportedRuleSelectors lists the codes, prefixes, and compatibility aliases
// accepted by the bundled Ruff release without preview mode.
func SupportedRuleSelectors() []string {
	return slices.Clone(supportedRuleSelectors)
}
