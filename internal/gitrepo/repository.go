// Package gitrepo reads a repository's configuration, index and ignore rules
// from disk without running git.
package gitrepo

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

type Repository struct {
	WorkTree   string
	GitDir     string
	CommonDir  string
	ignoreCase bool
	precompose bool
	hashLength int
	excludes   string
}

func Open(dir string) (*Repository, error) {
	workTree, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	for {
		dotGit := filepath.Join(workTree, ".git")
		info, err := os.Stat(dotGit)
		switch {
		case err == nil:
			return open(workTree, dotGit, info)
		case !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR):
			return nil, err
		}
		parent := filepath.Dir(workTree)
		if parent == workTree {
			return nil, nil
		}
		workTree = parent
	}
}

func open(workTree, dotGit string, info fs.FileInfo) (*Repository, error) {
	repo := &Repository{WorkTree: workTree, hashLength: 20}
	switch {
	case info.IsDir():
		repo.GitDir = dotGit
	case info.Mode().IsRegular():
		data, err := os.ReadFile(dotGit)
		if err != nil {
			return nil, err
		}
		target, ok := strings.CutPrefix(strings.TrimRight(string(data), "\r\n"), "gitdir: ")
		if !ok {
			return nil, fmt.Errorf("%s has no gitdir line", dotGit)
		}
		repo.GitDir = filepath.FromSlash(target)
		if !filepath.IsAbs(repo.GitDir) {
			repo.GitDir = filepath.Join(workTree, repo.GitDir)
		}
		repo.GitDir = filepath.Clean(repo.GitDir)
		if gitInfo, err := os.Stat(repo.GitDir); err != nil {
			return nil, err
		} else if !gitInfo.IsDir() {
			return nil, fmt.Errorf("%s is not a directory", repo.GitDir)
		}
	default:
		return nil, fmt.Errorf("%s is neither a directory nor a file", dotGit)
	}
	repo.CommonDir = repo.GitDir
	common, err := readOptional(filepath.Join(repo.GitDir, "commondir"))
	if err != nil {
		return nil, err
	}
	if common != nil {
		repo.CommonDir = filepath.FromSlash(strings.TrimRight(string(common), "\r\n"))
		if !filepath.IsAbs(repo.CommonDir) {
			repo.CommonDir = filepath.Join(repo.GitDir, repo.CommonDir)
		}
		repo.CommonDir = filepath.Clean(repo.CommonDir)
	}
	format, err := readRepositoryFormat(filepath.Join(repo.CommonDir, "config"))
	if err != nil {
		return nil, err
	}
	if format.sha256 {
		repo.hashLength = 32
	}
	settings, err := repo.config(format)
	if err != nil {
		return nil, err
	}
	if repo.ignoreCase, err = settings.boolean("core.ignorecase"); err != nil {
		return nil, err
	}
	if repo.precompose, err = settings.boolean("core.precomposeunicode"); err != nil {
		return nil, err
	}
	excludes, configured, err := repo.excludesFile(settings["core.excludesfile"])
	if err != nil {
		return nil, err
	}
	if configured {
		repo.excludes = excludes
	} else if xdg, ok := xdgConfigHome(); ok {
		repo.excludes = filepath.Join(xdg, "git", "ignore")
	}
	return repo, nil
}

func (r *Repository) IgnoreCase() bool {
	return r.ignoreCase
}

func (r *Repository) PrecomposeUnicode() bool {
	return r.precompose
}

// git reads one system file fixed at build time; distribution builds use prefix /usr and ETC_GITCONFIG /etc/gitconfig.
const (
	gitPrefix    = "/usr"
	systemConfig = "/etc/gitconfig"
)

func (r *Repository) config(format repositoryFormat) (config, error) {
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
			system = systemConfig
		}
		files = append(files, system)
	}
	global, ok := os.LookupEnv("GIT_CONFIG_GLOBAL")
	globals := []string{global}
	if !ok {
		globals = nil
		if xdg, ok := xdgConfigHome(); ok {
			globals = append(globals, filepath.Join(xdg, "git", "config"))
		}
		if home, ok := homeDir(); ok {
			globals = append(globals, filepath.Join(home, ".gitconfig"))
		}
	}
	for _, file := range globals {
		if f, err := os.Open(file); err == nil {
			_ = f.Close()
		} else if errors.Is(err, fs.ErrPermission) {
			continue
		}
		files = append(files, file)
	}
	files = append(files, filepath.Join(r.CommonDir, "config"))
	if format.worktreeConfig {
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
	ctx, err := newIncludeContext(r.GitDir, r.WorkTree)
	if err != nil {
		return nil, err
	}
	return loadConfigFiles(files, params, ctx)
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
		if expanded != "" && !isAbsolutePath(expanded) {
			expanded = filepath.Join(r.WorkTree, expanded)
		}
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

func readOptional(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return nil, nil
	}
	return data, err
}

const maxPatternFileSize = 100 << 20

func readNoFollow(path string) []byte {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&fs.ModeSymlink != 0 {
		return nil
	}
	data, err := readPatternFile(path)
	if err != nil {
		return nil
	}
	return data
}

func readExcludeFile(path string) ([]byte, error) {
	if !readable(path) {
		return nil, nil
	}
	data, err := readPatternFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot use %s as an exclude file: %w", path, err)
	}
	return data, nil
}

func readPatternFile(path string) ([]byte, error) {
	file, err := os.Open(path)
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
