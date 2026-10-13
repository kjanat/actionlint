package projectpath

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"

	"actionlint.kjanat.dev/internal/gitrepo"
)

// Actions discovers manifests without following symlinks or crossing repository
// boundaries. Skip conventional dependency, cache and test-fixture directories.
func Actions(ctx context.Context, root string, onRead func(path string)) ([]string, error) {
	physical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("could not resolve action discovery root %q: %w", root, err)
	}
	relatives, err := discoverActions(ctx, root, physical, onRead)
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
	root       string
	prefix     string
	ignores    *gitrepo.Ignores
	tracked    []string
	ignoreCase bool
	precompose bool
}

func discoverActions(ctx context.Context, root, physical string, onRead func(path string)) ([]string, error) {
	repo, err := gitrepo.Open(root)
	if err != nil {
		return nil, err
	}
	discovery := &actionDiscovery{root: physical}
	if repo != nil && repo.WorkTree != "" {
		if discovery, err = newActionDiscovery(ctx, physical, repo, runtime.GOOS == "darwin" && repo.PrecomposeUnicode()); err != nil {
			return nil, err
		}
	}
	relatives, err := discovery.walk(ctx)
	if err != nil {
		return nil, err
	}
	if repo != nil && onRead != nil {
		for _, path := range repo.ReadFiles() {
			onRead(path)
		}
	}
	return relatives, nil
}

func insideWorkTree(workTree, path string) (string, bool) {
	relative, err := filepath.Rel(workTree, path)
	if err != nil {
		return "", false
	}
	return relative, relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func newActionDiscovery(ctx context.Context, physical string, repo *gitrepo.Repository, precompose bool) (*actionDiscovery, error) {
	discovery := &actionDiscovery{root: physical, ignoreCase: repo.IgnoreCase(), precompose: precompose}
	relative, inside := insideWorkTree(repo.WorkTree, physical)
	if !inside {
		return &actionDiscovery{root: physical}, nil
	}
	if relative != "." {
		discovery.prefix = discovery.compose(filepath.ToSlash(relative)) + "/"
	}
	ignores, err := repo.Ignores()
	if err != nil {
		return nil, err
	}
	discovery.ignores = ignores
	tracked, err := repo.TrackedFiles(ctx)
	if err != nil {
		return nil, err
	}
	discovery.tracked = make([]string, 0, len(tracked))
	for _, path := range tracked {
		discovery.tracked = append(discovery.tracked, discovery.fold(path))
	}
	slices.Sort(discovery.tracked)
	return discovery, nil
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
		treePath := d.prefix + d.compose(filepath.ToSlash(relative))
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
			if d.ignores.Ignored(treePath, true) && !d.trackedUnder(treePath) {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || !isManifest(entry.Name()) {
			return nil
		}
		if d.ignores != nil && !d.isTracked(treePath) && d.ignores.Ignored(treePath, false) {
			return nil
		}
		relatives = append(relatives, relative)
		return nil
	})
	return relatives, err
}

func (d *actionDiscovery) compose(treePath string) string {
	if !d.precompose {
		return treePath
	}
	return norm.NFC.String(treePath)
}

func (d *actionDiscovery) fold(treePath string) string {
	if !d.ignoreCase {
		return treePath
	}
	folded := []byte(treePath)
	for i, b := range folded {
		if 'A' <= b && b <= 'Z' {
			folded[i] = b + 'a' - 'A'
		}
	}
	return string(folded)
}

func (d *actionDiscovery) isTracked(treePath string) bool {
	_, found := slices.BinarySearch(d.tracked, d.fold(treePath))
	return found
}

func (d *actionDiscovery) trackedUnder(treePath string) bool {
	prefix := d.fold(treePath) + "/"
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
