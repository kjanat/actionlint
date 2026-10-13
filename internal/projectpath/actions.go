package projectpath

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// skippedActionDirs lists directory names that are never searched for action
// manifests: version control, dependencies, caches and test fixtures.
var skippedActionDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	".venv":        true,
	"venv":         true,
	".cache":       true,
	".worktrees":   true,
	"testdata":     true,
}

// Actions discovers manifests without following symlinks or crossing repository
// boundaries. Skip conventional dependency, cache and test-fixture directories.
// Inside a Git work tree the file list also honours Git's ignore rules
// (.gitignore, .git/info/exclude and the global excludes file), so a manifest in
// an ignored directory — for example a vendored copy of another repository — is
// no longer linted. Outside a repository, or without a Git binary, it falls back
// to walking the tree.
func Actions(root string) ([]string, error) {
	physical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("could not resolve action discovery root %q: %w", root, err)
	}
	if visible, ok := gitVisibleFiles(physical); ok {
		return visibleActions(root, physical, visible), nil
	}
	return walkActions(root, physical)
}

// gitVisibleFiles lists the files Git would not ignore, relative to root, using
// `git ls-files --cached --others --exclude-standard`. The boolean is false when
// root is not inside a Git work tree or Git is unavailable, so the caller can
// fall back to a plain walk.
func gitVisibleFiles(root string) ([]string, bool) {
	cmd := exec.Command("git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	var out bytes.Buffer
	cmd.Stdout = &out
	// Stderr stays nil: Git reports "not a repository" through the exit status.
	if err := cmd.Run(); err != nil {
		return nil, false
	}
	fields := strings.Split(out.String(), "\x00")
	files := make([]string, 0, len(fields))
	for _, field := range fields {
		if field != "" {
			files = append(files, filepath.FromSlash(field))
		}
	}
	return files, true
}

// visibleActions filters a Git file list down to the action manifests that the
// walk would also report: regular files, outside the skipped directories and
// outside nested repositories.
func visibleActions(root, physical string, visible []string) []string {
	var paths []string
	for _, relative := range visible {
		switch filepath.Base(relative) {
		case "action.yml", "action.yaml":
		default:
			continue
		}
		if skipActionPath(relative) {
			continue
		}
		full := filepath.Join(physical, relative)
		info, err := os.Lstat(full)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if insideNestedRepository(physical, filepath.Dir(full)) {
			continue
		}
		paths = append(paths, filepath.Join(root, relative))
	}
	return paths
}

// skipActionPath reports whether a repository-relative path is excluded by the
// conventional directory list or lives under the workflow directory.
func skipActionPath(relative string) bool {
	relative = filepath.ToSlash(relative)
	if strings.HasPrefix(relative, ".github/workflows/") {
		return true
	}
	for _, segment := range strings.Split(relative, "/") {
		if skippedActionDirs[segment] {
			return true
		}
	}
	return false
}

// insideNestedRepository reports whether dir, which must be at or below root,
// belongs to a repository other than root's.
func insideNestedRepository(root, dir string) bool {
	for dir != root && strings.HasPrefix(dir, root+string(filepath.Separator)) {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		dir = filepath.Dir(dir)
	}
	return false
}

// walkActions is the original tree walk, kept for the cases where Git cannot be
// consulted.
func walkActions(root, physical string) ([]string, error) {
	var paths []string
	workflows := filepath.Join(physical, ".github", "workflows")
	err := filepath.WalkDir(physical, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path == physical {
				return nil
			}
			if path == workflows {
				return filepath.SkipDir
			}
			if skippedActionDirs[entry.Name()] {
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
