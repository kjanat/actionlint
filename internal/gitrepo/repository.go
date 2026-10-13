// Package gitrepo reads a repository's configuration, index and ignore rules
// from disk without running git.
package gitrepo

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
	settings, err := repo.config()
	if err != nil {
		return nil, err
	}
	if repo.ignoreCase, err = settings.boolean("core.ignorecase"); err != nil {
		return nil, err
	}
	if repo.precompose, err = settings.boolean("core.precomposeunicode"); err != nil {
		return nil, err
	}
	switch format := strings.ToLower(settings["extensions.objectformat"]); format {
	case "", "sha1":
	case "sha256":
		repo.hashLength = 32
	default:
		return nil, fmt.Errorf("unknown object format %q", format)
	}
	if configured, ok := settings["core.excludesfile"]; ok {
		repo.excludes = repo.excludesFile(configured)
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

func (r *Repository) config() (config, error) {
	var files []string
	noSystem, err := parseBool(os.Getenv("GIT_CONFIG_NOSYSTEM"))
	if err != nil {
		return nil, fmt.Errorf("GIT_CONFIG_NOSYSTEM: %w", err)
	}
	if !noSystem {
		files = append(files, envOr("GIT_CONFIG_SYSTEM", "/etc/gitconfig"))
	}
	if global := os.Getenv("GIT_CONFIG_GLOBAL"); global != "" {
		files = append(files, global)
	} else {
		if xdg, ok := xdgConfigHome(); ok {
			files = append(files, filepath.Join(xdg, "git", "config"))
		}
		if home, ok := homeDir(); ok {
			files = append(files, filepath.Join(home, ".gitconfig"))
		}
	}
	files = append(files, filepath.Join(r.CommonDir, "config"))
	ctx, err := newIncludeContext(r.GitDir, r.WorkTree)
	if err != nil {
		return nil, err
	}
	settings, err := loadConfigFiles(files, ctx)
	if err != nil {
		return nil, err
	}
	worktreeConfig, err := settings.boolean("extensions.worktreeconfig")
	if err != nil {
		return nil, err
	}
	if worktreeConfig {
		files = append(files, filepath.Join(r.GitDir, "config.worktree"))
		if settings, err = loadConfigFiles(files, ctx); err != nil {
			return nil, err
		}
	}
	return settings, nil
}

func (r *Repository) excludesFile(configured string) string {
	if configured == "" {
		return ""
	}
	expanded, ok := expandHome(configured)
	if !ok {
		return ""
	}
	if isAbsolutePath(expanded) {
		return filepath.FromSlash(expanded)
	}
	return filepath.Join(r.WorkTree, filepath.FromSlash(expanded))
}

func xdgConfigHome() (string, bool) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return xdg, true
	}
	home, ok := homeDir()
	if !ok {
		return "", false
	}
	return filepath.Join(home, ".config"), true
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func readOptional(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return nil, nil
	}
	return data, err
}

func readNoFollow(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, nil
	}
	return readOptional(path)
}
