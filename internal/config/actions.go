package config

import (
	"path"
	"strings"
)

// SplitActionRef separates an action reference into its name and ref.
func SplitActionRef(s string) (string, string) {
	name, ref, _ := strings.Cut(s, "@")
	return name, ref
}

// InvalidActionPattern returns the half of a required-actions entry which path.Match
// rejects as a pattern. The second return value is false when both halves are valid.
func InvalidActionPattern(s string) (string, bool) {
	name, ref := SplitActionRef(s)
	for _, p := range []string{name, ref} {
		if _, err := path.Match(p, ""); err != nil {
			return p, true
		}
	}
	return "", false
}
