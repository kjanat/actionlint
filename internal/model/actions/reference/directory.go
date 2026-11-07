// Package reference validates source references for local actions.
package reference

import (
	"fmt"
	"path"
	"strings"
)

// NormalizeLocalDirectory returns a canonical repository-relative directory reference.
func NormalizeLocalDirectory(value string) (string, error) {
	if !strings.HasPrefix(value, "./") && !strings.HasPrefix(value, "$/") {
		return "", fmt.Errorf("action directory %q must start with ./ or $/", value)
	}
	relative := value[2:]
	if strings.ContainsAny(relative, "\\\x00\r\n") || strings.Contains(relative, "${{") || strings.Contains(relative, ":") {
		return "", fmt.Errorf("action directory %q must be a literal repository-relative path", value)
	}
	relative = path.Clean(relative)
	if relative == ".." || strings.HasPrefix(relative, "../") || strings.HasPrefix(relative, "/") {
		return "", fmt.Errorf("action directory %q escapes the repository", value)
	}
	if relative == "." {
		return "./", nil
	}
	return "./" + relative, nil
}
