// Package displaypath chooses how a file path is shown to a person.
package displaypath

import "path/filepath"

// FromDirectory returns path relative to directory, or path itself when the
// absolute spelling is shorter. A relative path is returned unchanged.
func FromDirectory(directory, path string) string {
	if !filepath.IsAbs(path) {
		return path
	}
	relative, err := filepath.Rel(directory, path)
	if err != nil || len(path) < len(relative) {
		return path
	}
	return relative
}
