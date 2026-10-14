package projectpath

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"actionlint.kjanat.dev/internal/fsread"
)

// Actions discovers manifests without following symlinks or crossing repository
// boundaries. Skip conventional dependency, cache and test-fixture directories.
func Actions(ctx context.Context, root string, reader *fsread.Reader) ([]string, error) {
	physical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("could not resolve action discovery root %q: %w", root, err)
	}
	logical := reader.Mapped(func(path string) string {
		relative, err := filepath.Rel(physical, path)
		if err != nil {
			return path
		}
		return filepath.Join(root, relative)
	})
	var paths []string
	workflows := filepath.Join(physical, ".github", "workflows")
	err = logical.WalkDir(physical, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if path == physical {
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
			relative, err := filepath.Rel(physical, path)
			if err != nil {
				return err
			}
			paths = append(paths, filepath.Join(root, relative))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("could not discover actions in %q: %w", root, err)
	}
	return paths, nil
}
