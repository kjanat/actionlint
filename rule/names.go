package rule

// CanonicalName resolves historical built-in IDs. Configuration and suppression
// boundaries accept aliases; diagnostics and generated reference material use
// the canonical name. Unknown names, including plugin IDs, pass through.
func CanonicalName(name string) string {
	switch name {
	case "require-commit-hash":
		return "unpinned-reference"
	case "require-job-timeout":
		return "job-timeout"
	case "require-permissions":
		return "explicit-permissions"
	case "require-action-metadata":
		return "missing-action-metadata"
	case "disallow-suppressions":
		return "forbidden-suppression"
	case "suspicious-coercion":
		return "coercion"
	default:
		return name
	}
}

// ValidCategory reports whether category is a semantic configuration group.
// Provider identity and maturity never change a rule's configuration path.
func ValidCategory(category string) bool {
	switch category {
	case "correctness", "suspicious", "security", "conventions":
		return true
	default:
		return false
	}
}
