// Package localaction validates source mappings for generated local actions.
package localaction

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Normalize returns a canonical repository-relative directory reference.
func Normalize(value string) (string, error) {
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

// CheckSource rejects mapped source directories and files that resolve outside the repository.
func CheckSource(root, source string) error {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(source)))
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || !filepath.IsLocal(relative) {
		return fmt.Errorf("source directory %q resolves outside the repository", source)
	}
	for _, name := range []string{"action.yml", "action.yaml", "Dockerfile", "dockerfile"} {
		manifest, err := filepath.EvalSymlinks(filepath.Join(resolved, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if name == "Dockerfile" || name == "dockerfile" {
			info, err := os.Stat(manifest)
			if err != nil {
				return err
			}
			if info.IsDir() {
				continue
			}
		}
		relative, err := filepath.Rel(resolvedRoot, manifest)
		if err != nil || !filepath.IsLocal(relative) {
			return fmt.Errorf("source file %q resolves outside the repository", filepath.Join(source, name))
		}
		break
	}
	return nil
}
