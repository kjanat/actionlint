package projectpath

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/execabs"

	"actionlint.kjanat.dev/internal/fsread"
)

// Actions discovers manifests without following symlinks or crossing repository
// boundaries. Skip conventional dependency, cache and test-fixture directories.
// Inside a Git repository, git decides which manifests and directories its
// ignore rules exclude, and tracked manifests in ignored directories stay discovered.
func Actions(ctx context.Context, root string, reader *fsread.Reader) ([]string, error) {
	physical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("could not resolve action discovery root %q: %w", root, err)
	}
	var filter *gitFilter
	if RepositoryRoot(physical) != "" {
		if filter, err = newGitFilter(ctx, physical, reader); err != nil {
			return nil, fmt.Errorf("could not discover actions in %q: %w", root, err)
		}
	}
	logical := reader.Mapped(func(path string) string {
		relative, err := filepath.Rel(physical, path)
		if err != nil {
			return path
		}
		return filepath.Join(root, relative)
	})
	relatives, err := walkActions(ctx, physical, logical, filter)
	if err != nil {
		return nil, fmt.Errorf("could not discover actions in %q: %w", root, err)
	}
	paths := make([]string, 0, len(relatives))
	for _, relative := range relatives {
		paths = append(paths, filepath.Join(root, relative))
	}
	return paths, nil
}

type gitFilter struct {
	manifests map[string]bool
	ignored   map[string]bool
}

func newGitFilter(ctx context.Context, root string, reader *fsread.Reader) (*gitFilter, error) {
	git, err := execabs.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("git is required to apply ignore rules inside a repository: %w", err)
	}
	run := func(args ...string) ([]byte, error) {
		// Repository-configured fsmonitor commands must not run during discovery.
		command := exec.CommandContext(ctx, git, append([]string{"-C", root, "-c", "core.fsmonitor=false"}, args...)...)
		command.Env = gitEnvironment()
		var stderr strings.Builder
		command.Stderr = &stderr
		output, err := command.Output()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		message := strings.TrimSpace(stderr.String())
		if err != nil {
			return nil, fmt.Errorf("git %s: %w: %s", args[0], err, message)
		}
		// Git skips an unreadable directory with a warning and exit status 0.
		if message != "" {
			return nil, fmt.Errorf("git %s: %s", args[0], message)
		}
		return output, nil
	}
	manifests, err := run("ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", ":(glob)**/action.yml", ":(glob)**/action.yaml")
	if err != nil {
		return nil, err
	}
	ignored, err := run("ls-files", "-z", "--others", "--ignored", "--exclude-standard", "--directory")
	if err != nil {
		return nil, err
	}
	locations, err := run("rev-parse", "--path-format=absolute", "--show-toplevel", "--git-path", "info/exclude", "--git-path", "index")
	if err != nil {
		return nil, err
	}
	excludes, err := run("config", "--path", "--default", defaultExcludesFile(), "--get", "core.excludesFile")
	if err != nil {
		return nil, err
	}
	var read []string
	for location := range strings.Lines(string(locations) + string(excludes)) {
		if location = strings.TrimRight(location, "\r\n"); location != "" {
			read = append(read, location)
		}
	}
	if len(read) == 0 {
		return nil, fmt.Errorf("git rev-parse printed no work tree for %q", root)
	}
	top := filepath.Clean(read[0])
	read = read[1:]
	for dir := root; dir != top; {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
		read = append(read, filepath.Join(dir, ".gitignore"))
	}
	for _, location := range read {
		if !filepath.IsAbs(location) {
			location = filepath.Join(root, location)
		}
		if info, err := os.Stat(location); err == nil && info.Mode().IsRegular() {
			reader.Record(location)
		}
	}
	filter := &gitFilter{manifests: map[string]bool{}, ignored: map[string]bool{}}
	for name := range bytes.SplitSeq(manifests, []byte{0}) {
		if len(name) > 0 {
			filter.manifests[string(name)] = true
		}
	}
	for name := range bytes.SplitSeq(ignored, []byte{0}) {
		if directory, ok := bytes.CutSuffix(name, []byte{'/'}); ok {
			filter.ignored[string(directory)] = true
		}
	}
	return filter, nil
}

// gitignore(5) names this file when core.excludesFile is unset.
func defaultExcludesFile() string {
	if config := os.Getenv("XDG_CONFIG_HOME"); config != "" {
		return filepath.Join(config, "git", "ignore")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "git", "ignore")
}

func gitEnvironment() []string {
	var env []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		name = strings.ToUpper(name)
		switch {
		case name == "GIT_DIR", name == "GIT_WORK_TREE", name == "GIT_INDEX_FILE", name == "GIT_COMMON_DIR":
		case name == "GIT_LITERAL_PATHSPECS", name == "GIT_GLOB_PATHSPECS", name == "GIT_NOGLOB_PATHSPECS", name == "GIT_ICASE_PATHSPECS":
		case strings.HasPrefix(name, "GIT_TRACE"):
		default:
			env = append(env, entry)
		}
	}
	return env
}

func walkActions(ctx context.Context, root string, reader *fsread.Reader, filter *gitFilter) ([]string, error) {
	var relatives []string
	err := reader.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			skip, err := skipDirectory(root, path)
			if err != nil {
				return err
			}
			if skip || filter != nil && filter.ignored[filepath.ToSlash(relative)] {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if filter != nil && entry.Name() == ".gitignore" {
			reader.Record(path)
		}
		if isManifest(entry.Name()) && (filter == nil || filter.manifests[filepath.ToSlash(relative)]) {
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
