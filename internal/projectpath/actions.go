package projectpath

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"actionlint.kjanat.dev/internal/gitrepo"
)

// Actions discovers manifests without following symlinks or crossing repository
// boundaries. Skip conventional dependency, cache and test-fixture directories.
func Actions(ctx context.Context, root string) ([]string, error) {
	physical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("could not resolve action discovery root %q: %w", root, err)
	}
	relatives, err := discoverActions(ctx, physical)
	if err != nil {
		return nil, fmt.Errorf("could not discover actions in %q: %w", root, err)
	}
	paths := make([]string, 0, len(relatives))
	for _, relative := range relatives {
		paths = append(paths, filepath.Join(root, relative))
	}
	return paths, nil
}

type actionDiscovery struct {
	root    string
	prefix  string
	ignores *gitrepo.Ignores
	tracked []string
}

func discoverActions(ctx context.Context, physical string) ([]string, error) {
	repo, err := gitrepo.Open(physical)
	if err != nil {
		return nil, err
	}
	discovery := &actionDiscovery{root: physical}
	if repo != nil {
		relative, err := filepath.Rel(repo.WorkTree, physical)
		if err != nil {
			return nil, err
		}
		if relative != "." {
			discovery.prefix = filepath.ToSlash(relative) + "/"
		}
		if discovery.ignores, err = repo.Ignores(); err != nil {
			return nil, err
		}
		if discovery.tracked, err = repo.TrackedFiles(); err != nil {
			return nil, err
		}
	}
	return discovery.walk(ctx)
}

func (d *actionDiscovery) walk(ctx context.Context) ([]string, error) {
	var relatives []string
	err := filepath.WalkDir(d.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == d.root {
			return nil
		}
		relative, err := filepath.Rel(d.root, path)
		if err != nil {
			return err
		}
		treePath := d.prefix + filepath.ToSlash(relative)
		if entry.IsDir() {
			skip, err := skipDirectory(d.root, path)
			if err != nil {
				return err
			}
			if skip {
				return filepath.SkipDir
			}
			if d.ignores == nil {
				return nil
			}
			ignored, err := d.ignores.Ignored(treePath, true)
			if err != nil {
				return err
			}
			if ignored && !d.trackedUnder(treePath) {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || !isManifest(entry.Name()) {
			return nil
		}
		if d.ignores != nil && !d.isTracked(treePath) {
			ignored, err := d.ignores.Ignored(treePath, false)
			if err != nil {
				return err
			}
			if ignored {
				return nil
			}
		}
		relatives = append(relatives, relative)
		return nil
	})
	return relatives, err
}

func (d *actionDiscovery) isTracked(treePath string) bool {
	_, found := slices.BinarySearch(d.tracked, treePath)
	return found
}

func (d *actionDiscovery) trackedUnder(treePath string) bool {
	prefix := treePath + "/"
	index := sort.SearchStrings(d.tracked, prefix)
	return index < len(d.tracked) && strings.HasPrefix(d.tracked[index], prefix)
}

func skipDirectory(root, path string) (bool, error) {
	if path == filepath.Join(root, ".github", "workflows") {
		return true, nil
	}
	switch filepath.Base(path) {
	case ".git", "node_modules", "vendor", ".venv", "venv", ".cache", ".worktrees", "testdata":
		return true, nil
	}
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		return true, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	return false, nil
}

func isManifest(name string) bool {
	return name == "action.yml" || name == "action.yaml"
}
