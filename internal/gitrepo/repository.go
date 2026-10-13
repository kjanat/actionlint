// Package gitrepo reads a repository's configuration, index and ignore rules
// from disk without running git.
package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
)

type Repository struct {
	WorkTree         string
	GitDir           string
	CommonDir        string
	ignoreCase       bool
	precompose       bool
	worktreeConfig   bool
	reftable         bool
	partialClone     string
	hashLength       int
	excludes         string
	unresolvedGitDir string
	base             string
	reads            fileReads
}

type fileReads struct {
	mu    sync.Mutex
	paths map[string]struct{}
}

func (f *fileReads) record(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.paths == nil {
		f.paths = map[string]struct{}{}
	}
	f.paths[path] = struct{}{}
}

func (r *Repository) ReadFiles() []string {
	r.reads.mu.Lock()
	defer r.reads.mu.Unlock()
	paths := make([]string, 0, len(r.reads.paths))
	for path := range r.reads.paths {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return paths
}

type location struct {
	start      string
	dir        string
	gitDir     string
	unresolved string
	bare       bool
}

var (
	errNoGitFile    = errors.New("no gitfile")
	errGitFileIsDir = errors.New("gitfile is a directory")
)

// Open ignores GIT_DIR, GIT_WORK_TREE, GIT_INDEX_FILE, GIT_COMMON_DIR and GIT_OBJECT_DIRECTORY, which git exports to hooks for the hook's own repository.
func Open(ctx context.Context, dir string) (*Repository, error) {
	logical, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	start, err := filepath.EvalSymlinks(logical)
	if err != nil {
		return nil, err
	}
	repo := &Repository{hashLength: 20}
	d := repo.disk(ctx)
	found, err := discover(d, start)
	if err != nil || found == nil {
		return nil, err
	}
	found.start = start
	found.unresolved = found.gitDir
	if runtime.GOOS != "windows" && found.dir == start && found.gitDir == filepath.Join(start, ".git") {
		found.unresolved = filepath.Join(logical, ".git")
	}
	if err := repo.load(d, found); err != nil {
		return nil, err
	}
	return repo, nil
}

func discover(d disk, dir string) (*location, error) {
	minOffset := rootLength(dir)
	ceiling := ceilingLength(dir)
	if ceiling < 0 {
		ceiling = minOffset - 2
	}
	acrossFilesystems, err := parseBool(os.Getenv("GIT_DISCOVERY_ACROSS_FILESYSTEM"))
	if err != nil {
		return nil, fmt.Errorf("GIT_DISCOVERY_ACROSS_FILESYSTEM: %w", err)
	}
	var device uint64
	if !acrossFilesystems {
		if device, err = deviceID(dir); err != nil {
			return nil, err
		}
	}
	for {
		offset := len(dir)
		dotGit := dir + ".git"
		if offset > minOffset {
			dotGit = joinRaw(dir, ".git")
		}
		gitDir, err := readGitFile(d, dotGit)
		switch {
		case err == nil:
			return &location{dir: dir, gitDir: gitDir}, nil
		case errors.Is(err, errGitFileIsDir):
			ok, err := isGitDirectory(d, dotGit)
			if err != nil {
				return nil, err
			}
			if ok {
				return &location{dir: dir, gitDir: dotGit}, nil
			}
		case !errors.Is(err, errNoGitFile):
			return nil, err
		}
		ok, err := isGitDirectory(d, dir)
		if err != nil {
			return nil, err
		}
		if ok {
			return &location{dir: dir, gitDir: dir, bare: true}, nil
		}
		if offset <= minOffset {
			return nil, nil
		}
		for offset--; offset > ceiling && !os.IsPathSeparator(dir[offset]); offset-- {
		}
		if offset <= ceiling {
			return nil, nil
		}
		dir = dir[:max(offset, minOffset)]
		if !acrossFilesystems {
			current, err := deviceID(dir)
			if err != nil {
				return nil, err
			}
			if current != device {
				return nil, nil
			}
		}
	}
}

func rootLength(path string) int {
	length := len(filepath.VolumeName(path))
	if length < len(path) && os.IsPathSeparator(path[length]) {
		length++
	}
	return length
}

func ceilingLength(dir string) int {
	value, ok := os.LookupEnv("GIT_CEILING_DIRECTORIES")
	path := filepath.ToSlash(dir)
	if !ok || path == "/" {
		return -1
	}
	longest := -1
	verbatim := false
	for ceiling := range strings.SplitSeq(value, string(os.PathListSeparator)) {
		switch {
		case ceiling == "":
			verbatim = true
			continue
		case !isAbsolutePath(ceiling):
			continue
		case !verbatim:
			resolved, err := filepath.EvalSymlinks(ceiling)
			if err != nil {
				continue
			}
			ceiling = resolved
		}
		ceiling = filepath.ToSlash(ceiling)
		length := len(strings.TrimSuffix(ceiling, "/"))
		if len(path) > length+1 && path[length] == '/' && path[:length] == ceiling[:length] {
			longest = max(longest, length)
		}
	}
	return longest
}

func joinRaw(dir, name string) string {
	if dir != "" && os.IsPathSeparator(dir[len(dir)-1]) {
		return dir + name
	}
	return dir + string(os.PathSeparator) + name
}

func readGitFile(d disk, path string) (string, error) {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR):
		return "", errNoGitFile
	case err != nil:
		return "", err
	case info.IsDir():
		return "", errGitFileIsDir
	case !info.Mode().IsRegular():
		return "", fmt.Errorf("not a regular file: %s", path)
	case info.Size() > 1<<20:
		return "", fmt.Errorf("too large to be a .git file: %s", path)
	}
	data, err := d.readFile(path)
	if err != nil {
		return "", err
	}
	target, ok := strings.CutPrefix(string(data), "gitdir: ")
	if !ok {
		return "", fmt.Errorf("invalid gitfile format: %s", path)
	}
	if target = strings.TrimRight(target, "\r\n"); target == "" {
		return "", fmt.Errorf("no path in gitfile: %s", path)
	}
	target, _, _ = strings.Cut(target, "\x00")
	if target = filepath.FromSlash(target); !isAbsolutePath(target) {
		target = joinRaw(filepath.Dir(path), target)
	}
	if ok, err = isGitDirectory(d, target); err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("gitfile does not point to a valid repository: %s", path)
	}
	return filepath.EvalSymlinks(target)
}

func isGitDirectory(d disk, suspect string) (bool, error) {
	if !validHead(d, joinRaw(suspect, "HEAD")) {
		if err := d.ctx.Err(); err != nil {
			return false, err
		}
		return false, nil
	}
	common, _, err := commonDir(d, suspect)
	if err != nil {
		return false, err
	}
	return searchable(joinRaw(common, "objects")) && searchable(joinRaw(common, "refs")), nil
}

func validHead(d disk, path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		target, err := d.readlink(path)
		return err == nil && strings.HasPrefix(filepath.ToSlash(target), "refs/")
	}
	file, err := d.open(path)
	if err != nil {
		return false
	}
	buffer := make([]byte, 255)
	n, err := io.ReadFull(file, buffer)
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		err = nil
	}
	if err = errors.Join(err, file.Close()); err != nil {
		return false
	}
	head, _, _ := bytes.Cut(buffer[:n], []byte{0})
	if ref, ok := bytes.CutPrefix(head, []byte("ref:")); ok && bytes.HasPrefix(bytes.TrimLeft(ref, " \t\n\v\f\r"), []byte("refs/")) {
		return true
	}
	return hexPrefix(head, 64) || hexPrefix(head, 40)
}

func hexPrefix(data []byte, length int) bool {
	if len(data) < length {
		return false
	}
	for _, b := range data[:length] {
		if hexValue(b) >= 16 {
			return false
		}
	}
	return true
}

func commonDir(d disk, gitDir string) (string, bool, error) {
	path := joinRaw(gitDir, "commondir")
	if !exists(path) {
		return gitDir, false, nil
	}
	data, err := d.readFile(path)
	if err == nil && len(data) == 0 {
		err = errors.New("the file is empty")
	}
	if err != nil {
		return "", false, fmt.Errorf("failed to read %s: %w", path, err)
	}
	common, _, _ := strings.Cut(strings.TrimRight(string(data), "\r\n"), "\x00")
	if common = filepath.FromSlash(common); !isAbsolutePath(common) {
		common = joinRaw(gitDir, common)
	}
	resolved, err := filepath.EvalSymlinks(common)
	if err != nil {
		return "", false, err
	}
	return resolved, true, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (r *Repository) load(d disk, found *location) error {
	r.GitDir, r.unresolvedGitDir = found.gitDir, found.unresolved
	common, shared, err := commonDir(d, found.gitDir)
	if err != nil {
		return err
	}
	r.CommonDir = common
	format, err := readDiscoveryFormat(d, r.GitDir, r.CommonDir, shared)
	if err != nil {
		return err
	}
	if r.WorkTree, err = format.resolveWorkTree(found); err != nil {
		return err
	}
	r.base = found.start
	if relative, err := filepath.Rel(r.WorkTree, found.start); r.WorkTree != "" && err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		r.base = r.WorkTree
	}
	if format.sha256 {
		r.hashLength = 32
	}
	r.worktreeConfig = format.worktreeConfig
	r.reftable = format.refStorage == "reftable"
	r.partialClone = format.partialClone
	settings, err := r.config(d.ctx)
	if err != nil {
		return err
	}
	if r.ignoreCase, err = settings.boolean("core.ignorecase"); err != nil {
		return err
	}
	if r.precompose, err = settings.boolean("core.precomposeunicode"); err != nil {
		return err
	}
	excludes, configured, err := r.excludesFile(settings["core.excludesfile"])
	if err != nil {
		return err
	}
	if configured {
		r.excludes = excludes
	} else if xdg, ok := xdgConfigHome(); ok {
		r.excludes = r.absolute(filepath.Join(xdg, "git", "ignore"))
	}
	return nil
}

func (r *Repository) absolute(path string) string {
	if path == "" || isAbsolutePath(path) {
		return path
	}
	return filepath.Join(r.base, path)
}

func (r *Repository) branch(d disk) (string, error) {
	branch := resolveBranch(func(name string) (refValue, error) {
		if r.reftable {
			return r.readReftableRef(d, name)
		}
		return r.readLooseRef(d, name)
	})
	if err := d.ctx.Err(); err != nil {
		return "", err
	}
	return branch, nil
}

func (r *Repository) IgnoreCase() bool {
	return r.ignoreCase
}

func (r *Repository) PrecomposeUnicode() bool {
	return r.precompose
}

func (r *Repository) config(ctx context.Context) (config, error) {
	d := r.disk(ctx)
	var files []string
	noSystem := false
	if env, ok := os.LookupEnv("GIT_CONFIG_NOSYSTEM"); ok {
		var err error
		if noSystem, err = parseBool(env); err != nil {
			return nil, fmt.Errorf("bad boolean environment value %q for 'GIT_CONFIG_NOSYSTEM'", env)
		}
	}
	if !noSystem {
		system, ok := os.LookupEnv("GIT_CONFIG_SYSTEM")
		if !ok {
			system = systemConfigPath()
		}
		files = append(files, r.absolute(system))
	}
	global, ok := os.LookupEnv("GIT_CONFIG_GLOBAL")
	globals := []string{global}
	if !ok {
		globals = globalConfigFiles()
	}
	for _, file := range globals {
		file = r.absolute(file)
		if permissionDenied(file) {
			continue
		}
		files = append(files, file)
	}
	files = append(files, filepath.Join(r.CommonDir, "config"))
	if r.worktreeConfig {
		files = append(files, filepath.Join(r.GitDir, "config.worktree"))
	}
	files = slices.DeleteFunc(files, func(file string) bool {
		info, err := os.Stat(file)
		return file == "" || (err == nil && info.IsDir())
	})
	params, err := commandConfig()
	if err != nil {
		return nil, fmt.Errorf("unable to parse command-line config: %w", err)
	}
	includes, err := newIncludeContext(r.GitDir, r.unresolvedGitDir, r.WorkTree, func() (string, error) { return r.branch(d) }, d)
	if err != nil {
		return nil, err
	}
	return loadConfigFiles(files, params, includes)
}

func (r *Repository) excludesFile(values []configValue) (string, bool, error) {
	path, configured := "", false
	for _, value := range values {
		if value.bare {
			return "", false, errors.New("missing value for 'core.excludesfile'")
		}
		text, optional := strings.CutPrefix(value.text, ":(optional)")
		expanded, ok := expandHome(text, false)
		if !ok {
			return "", false, fmt.Errorf("failed to expand user dir in: %q", text)
		}
		expanded = filepath.FromSlash(expanded)
		expanded = r.absolute(expanded)
		path, configured = expanded, true
		if !optional {
			continue
		}
		if _, err := os.Stat(expanded); errors.Is(err, fs.ErrNotExist) {
			path, configured = "", false
		} else if err != nil {
			return "", false, err
		}
	}
	return path, configured, nil
}

func globalConfigFiles() []string {
	var files []string
	if xdg, ok := xdgConfigHome(); ok {
		files = append(files, filepath.Join(xdg, "git", "config"))
	}
	if home, ok := homeDir(); ok {
		files = append(files, filepath.FromSlash(home+"/.gitconfig"))
	}
	return files
}

func xdgConfigHome() (string, bool) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return xdg, true
	}
	home, ok := homeDir()
	if !ok {
		return "", false
	}
	return home + "/.config", true
}

const maxPatternFileSize = 100 << 20

func readNoFollow(d disk, path string) []byte {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&fs.ModeSymlink != 0 {
		return nil
	}
	data, err := readPatternFile(d, path)
	if err != nil {
		return nil
	}
	return data
}

func readExcludeFile(d disk, path string) ([]byte, error) {
	if !readable(path) {
		return nil, nil
	}
	data, err := readPatternFile(d, path)
	if err != nil {
		return nil, fmt.Errorf("cannot use %s as an exclude file: %w", path, err)
	}
	return data, nil
}

func readPatternFile(d disk, path string) ([]byte, error) {
	file, err := d.open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() == 0 {
		return nil, err
	}
	if info.Size() >= maxPatternFileSize {
		return nil, fmt.Errorf("%s is larger than %d bytes", file.Name(), maxPatternFileSize-1)
	}
	data := make([]byte, info.Size())
	if _, err := io.ReadFull(file, data); err != nil {
		return nil, err
	}
	return data, nil
}
