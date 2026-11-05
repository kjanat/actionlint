package functions

import "strings"

// IsStringPredicate reports whether a built-in tests strings and returns a boolean.
func IsStringPredicate(name string) bool {
	switch strings.ToLower(name) {
	case "contains", "startswith", "endswith":
		return true
	default:
		return false
	}
}
