package projectpath

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Actions discovers manifests without following symlinks or crossing repository
// boundaries. Skip conventional dependency, cache and test-fixture directories.
func Actions(root string) ([]string, error) {
	var paths []string
	workflows := filepath.Join(root, ".github", "workflows")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path == root {
				return nil
			}
			if path == workflows {
				return filepath.SkipDir
			}
			switch entry.Name() {
			case ".git", "node_modules", "vendor", ".venv", "venv", ".cache", ".worktrees", "testdata":
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
				return filepath.SkipDir
			} else if !os.IsNotExist(err) {
				return err
			}
			return nil
		}
		if entry.Type().IsRegular() && (entry.Name() == "action.yml" || entry.Name() == "action.yaml") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("could not discover actions in %q: %w", root, err)
	}
	return paths, nil
}
