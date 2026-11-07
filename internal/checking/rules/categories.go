package rule

import "slices"

// Categories lists the semantic groups accepted in rule metadata and configuration.
// The returned slice belongs to the caller.
func Categories() []string {
	return []string{"correctness", "suspicious", "security", "conventions"}
}

// ValidCategory reports whether category is a semantic configuration group.
// Provider identity and maturity never change a rule's configuration path.
func ValidCategory(category string) bool {
	return slices.Contains(Categories(), category)
}
