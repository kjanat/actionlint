package local

import (
	"fmt"
	"os"
	"path/filepath"
)

// checkMappedSource rejects mapped source directories and files that resolve outside the repository.
func checkMappedSource(root, source string) error {
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
