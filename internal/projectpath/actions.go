package projectpath

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"golang.org/x/sys/execabs"
	"golang.org/x/text/unicode/norm"

	"actionlint.kjanat.dev/v2/internal/fsread"
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
	manifests  map[string]bool
	ignored    map[string]bool
	ignoreCase bool
	precompose bool
}

func newGitFilter(ctx context.Context, root string, reader *fsread.Reader) (*gitFilter, error) {
	git, err := execabs.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("git is required to apply ignore rules inside a repository: %w", err)
	}
	run := func(optional bool, args ...string) ([]byte, error) {
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
		var exit *exec.ExitError
		// git config exits 1 without output when a key is unset.
		if optional && message == "" && errors.As(err, &exit) && exit.ExitCode() == 1 {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("git %s: %w: %s", args[0], err, message)
		}
		// Git skips an unreadable directory with a warning and exit status 0.
		if message != "" {
			return nil, fmt.Errorf("git %s: %s", args[0], message)
		}
		return output, nil
	}
	path := func(optional bool, args ...string) (string, error) {
		output, err := run(optional, args...)
		return strings.TrimSuffix(strings.TrimSuffix(string(output), "\n"), "\r"), err
	}
	manifests, err := run(false, "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", ":(glob)**/action.yml", ":(glob)**/action.yaml")
	if err != nil {
		return nil, err
	}
	ignored, err := run(false, "ls-files", "-z", "--others", "--ignored", "--exclude-standard", "--directory")
	if err != nil {
		return nil, err
	}
	config, err := run(false, "config", "--list", "--show-origin", "-z")
	if err != nil {
		return nil, err
	}
	includes, err := run(true, "config", "--show-origin", "--path", "-z", "--get-all", "include.path")
	if err != nil {
		return nil, err
	}
	top, err := path(false, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	if top == "" {
		return nil, fmt.Errorf("git rev-parse printed no work tree for %q", root)
	}
	top = filepath.Clean(top)
	read := []string{filepath.Join(top, ".git")}
	for _, args := range [][]string{{"--git-path", "info/exclude"}, {"--git-path", "index"}, {"--shared-index-path"}} {
		location, err := path(false, append([]string{"rev-parse"}, args...)...)
		if err != nil {
			return nil, err
		}
		if location != "" && !filepath.IsAbs(location) {
			location = filepath.Join(root, location)
		}
		read = append(read, location)
	}
	for dir := root; dir != top; {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
		read = append(read, filepath.Join(dir, ".gitignore"))
	}
	settings := parseGitConfig(config, top)
	read = append(read, settings.files...)
	read = append(read, includedConfigFiles(includes, top)...)
	excludes, err := path(true, "config", "--path", "--get", "core.excludesFile")
	if err != nil {
		return nil, err
	}
	if excludes == "" {
		excludes = defaultExcludesFile()
	} else if !filepath.IsAbs(excludes) {
		excludes = filepath.Join(top, excludes)
	}
	read = append(read, excludes)
	for _, location := range read {
		// Git for Windows prints configured paths with forward slashes.
		location = filepath.Clean(location)
		if info, err := os.Stat(location); err == nil && info.Mode().IsRegular() {
			reader.Record(location)
		}
	}
	filter := &gitFilter{
		manifests: map[string]bool{}, ignored: map[string]bool{},
		ignoreCase: settings.ignoreCase, precompose: settings.precompose && runtime.GOOS == "darwin",
	}
	for name := range bytes.SplitSeq(manifests, []byte{0}) {
		if len(name) > 0 {
			filter.manifests[filter.key(string(name))] = true
		}
	}
	for name := range bytes.SplitSeq(ignored, []byte{0}) {
		if directory, ok := bytes.CutSuffix(name, []byte{'/'}); ok {
			filter.ignored[filter.key(string(directory))] = true
		}
	}
	return filter, nil
}

func (f *gitFilter) key(path string) string {
	// git ls-files prints NFC names on macOS when core.precomposeUnicode is set.
	if f.precompose {
		path = norm.NFC.String(path)
	}
	if f.ignoreCase {
		return strings.ToLower(path)
	}
	return path
}

type gitSettings struct {
	files      []string
	ignoreCase bool
	precompose bool
}

// git config --list --show-origin -z prints origin NUL key [LF value] NUL per entry.
func parseGitConfig(output []byte, top string) gitSettings {
	var settings gitSettings
	fields := strings.Split(string(output), "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		if file, ok := strings.CutPrefix(fields[i], "file:"); ok {
			if !filepath.IsAbs(file) {
				file = filepath.Join(top, file)
			}
			if !slices.Contains(settings.files, file) {
				settings.files = append(settings.files, file)
			}
		}
		key, value, hasValue := strings.Cut(fields[i+1], "\n")
		switch strings.ToLower(key) {
		case "core.ignorecase":
			settings.ignoreCase = gitBool(value, hasValue)
		case "core.precomposeunicode":
			settings.precompose = gitBool(value, hasValue)
		}
	}
	return settings
}

// git config --show-origin --path -z --get-all prints origin NUL value NUL per entry.
// A relative include path is relative to the file that names it.
func includedConfigFiles(output []byte, top string) []string {
	var files []string
	fields := strings.Split(string(output), "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		origin, ok := strings.CutPrefix(fields[i], "file:")
		value := fields[i+1]
		if !ok || value == "" {
			continue
		}
		if !filepath.IsAbs(origin) {
			origin = filepath.Join(top, origin)
		}
		if !filepath.IsAbs(value) {
			value = filepath.Join(filepath.Dir(origin), value)
		}
		files = append(files, value)
	}
	return files
}

func gitBool(value string, hasValue bool) bool {
	switch strings.ToLower(value) {
	case "true", "yes", "on", "1":
		return true
	}
	return !hasValue
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
		case name == "GIT_DIR", name == "GIT_WORK_TREE", name == "GIT_INDEX_FILE", name == "GIT_COMMON_DIR", name == "GIT_CEILING_DIRECTORIES", name == "GIT_DISCOVERY_ACROSS_FILESYSTEM":
		case name == "GIT_OBJECT_DIRECTORY", name == "GIT_ALTERNATE_OBJECT_DIRECTORIES":
		case name == "GIT_LITERAL_PATHSPECS", name == "GIT_GLOB_PATHSPECS", name == "GIT_NOGLOB_PATHSPECS", name == "GIT_ICASE_PATHSPECS":
		case strings.HasPrefix(name, "GIT_TRACE"):
		default:
			env = append(env, entry)
		}
	}
	// RepositoryRoot crosses filesystem boundaries, and git must find the same repository.
	return append(env, "GIT_DISCOVERY_ACROSS_FILESYSTEM=1")
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
			if skip || filter != nil && filter.ignored[filter.key(filepath.ToSlash(relative))] {
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
		if isManifest(entry.Name()) && (filter == nil || filter.manifests[filter.key(filepath.ToSlash(relative))]) {
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
