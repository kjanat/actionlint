package projectpath

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// Actions discovers manifests without following symlinks or crossing repository
// boundaries. Skip conventional dependency, cache and test-fixture directories.
func Actions(ctx context.Context, root string) ([]string, error) {
	physical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("could not resolve action discovery root %q: %w", root, err)
	}
	var relatives []string
	if names, ok := gitFiles(ctx, physical); ok {
		relatives, err = listedActions(physical, names)
	} else {
		relatives, err = walkActions(physical)
	}
	if err != nil {
		return nil, fmt.Errorf("could not discover actions in %q: %w", root, err)
	}
	paths := make([]string, 0, len(relatives))
	for _, relative := range relatives {
		paths = append(paths, filepath.Join(root, relative))
	}
	return paths, nil
}

func gitFiles(ctx context.Context, root string) ([]string, bool) {
	if RepositoryRoot(root) == "" {
		return nil, false
	}
	command := exec.CommandContext(ctx, "git", "-C", root, "-c", "core.fsmonitor=false", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(name) {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR":
			continue
		}
		command.Env = append(command.Env, entry)
	}
	output, err := command.Output()
	if err != nil {
		return nil, false
	}
	var names []string
	for name := range strings.SplitSeq(string(output), "\x00") {
		if name != "" {
			names = append(names, name)
		}
	}
	return names, true
}

func listedActions(root string, names []string) ([]string, error) {
	var relatives []string
	for _, name := range names {
		relative := filepath.FromSlash(name)
		if !isManifest(filepath.Base(relative)) {
			continue
		}
		path := filepath.Join(root, relative)
		if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
			continue
		}
		skip := false
		for dir := filepath.Dir(path); dir != root && !skip; dir = filepath.Dir(dir) {
			var err error
			if skip, err = skipDirectory(root, dir); err != nil {
				return nil, err
			}
		}
		if !skip {
			relatives = append(relatives, relative)
		}
	}
	slices.Sort(relatives)
	return slices.Compact(relatives), nil
}

func walkActions(root string) ([]string, error) {
	var relatives []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path == root {
				return nil
			}
			skip, err := skipDirectory(root, path)
			if err != nil {
				return err
			}
			if skip {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type().IsRegular() && isManifest(entry.Name()) {
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			relatives = append(relatives, relative)
		}
		return nil
	})
	return relatives, err
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
