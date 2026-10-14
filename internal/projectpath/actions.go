package projectpath

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sys/execabs"

	"actionlint.kjanat.dev/internal/fsread"
)

// Actions discovers manifests without following symlinks or crossing repository
// boundaries. Skip conventional dependency, cache and test-fixture directories.
// Inside a Git repository, git lists the candidates so ignore rules apply and
// tracked manifests in ignored directories stay discovered.
func Actions(ctx context.Context, root string, reader *fsread.Reader) ([]string, error) {
	physical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("could not resolve action discovery root %q: %w", root, err)
	}
	var relatives []string
	if RepositoryRoot(physical) == "" {
		relatives, err = walkActions(ctx, physical, reader.Mapped(func(path string) string {
			relative, err := filepath.Rel(physical, path)
			if err != nil {
				return path
			}
			return filepath.Join(root, relative)
		}))
	} else {
		relatives, err = gitActions(ctx, physical)
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

func gitActions(ctx context.Context, root string) ([]string, error) {
	git, err := execabs.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("git is required to apply ignore rules inside a repository: %w", err)
	}
	// Repository-configured fsmonitor commands must not run during discovery.
	command := exec.CommandContext(ctx, git, "-C", root, "-c", "core.fsmonitor=false", "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", ":(glob)**/action.yml", ":(glob)**/action.yaml")
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(name) {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR":
			continue
		}
		command.Env = append(command.Env, entry)
	}
	var stderr strings.Builder
	command.Stderr = &stderr
	output, err := command.Output()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	message := strings.TrimSpace(stderr.String())
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w: %s", err, message)
	}
	// Git skips an unreadable directory with a warning and exit status 0.
	if message != "" {
		return nil, fmt.Errorf("git ls-files: %s", message)
	}
	var relatives []string
	for name := range strings.SplitSeq(string(output), "\x00") {
		if name == "" {
			continue
		}
		relative := filepath.FromSlash(name)
		keep, err := listedManifest(root, relative)
		if err != nil {
			return nil, err
		}
		if keep {
			relatives = append(relatives, relative)
		}
	}
	slices.Sort(relatives)
	return slices.Compact(relatives), nil
}

func listedManifest(root, relative string) (bool, error) {
	if !isManifest(filepath.Base(relative)) {
		return false, nil
	}
	path := filepath.Join(root, relative)
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	for dir := filepath.Dir(path); dir != root; dir = filepath.Dir(dir) {
		parent, err := os.Lstat(dir)
		if err != nil {
			return false, err
		}
		if !parent.IsDir() {
			return false, nil
		}
		skip, err := skipDirectory(root, dir)
		if err != nil || skip {
			return false, err
		}
	}
	return true, nil
}

func walkActions(ctx context.Context, root string, reader *fsread.Reader) ([]string, error) {
	var relatives []string
	err := reader.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
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
