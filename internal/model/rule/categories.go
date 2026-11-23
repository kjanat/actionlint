package rule

import "slices"

// Categories returns a slice that belongs to the caller.
func Categories() []string {
	return []string{"correctness", "suspicious", "security", "conventions"}
}

func ValidCategory(category string) bool {
	return slices.Contains(Categories(), category)
}
