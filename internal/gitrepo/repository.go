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
		info, err := os.Lstat(dotGit)
		switch {
		case err == nil:
			return open(workTree, dotGit, info)
		case !errors.Is(err, fs.ErrNotExist):
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
		repo.CommonDir = filepath.Clean(filepath.Join(repo.GitDir, filepath.FromSlash(strings.TrimRight(string(common), "\r\n"))))
	}
	settings, err := repo.config()
	if err != nil {
		return nil, err
	}
	repo.ignoreCase = settings.truthy("core.ignorecase")
	switch format := strings.ToLower(settings["extensions.objectformat"]); format {
	case "", "sha1":
	case "sha256":
		repo.hashLength = 32
	default:
		return nil, fmt.Errorf("unknown object format %q", format)
	}
	repo.excludes = excludesFile(settings["core.excludesfile"])
	return repo, nil
}

func (r *Repository) config() (config, error) {
	settings := config{}
	var files []string
	if !truthyEnv("GIT_CONFIG_NOSYSTEM") {
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
	ctx, err := newIncludeContext(r.GitDir)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if err := loadConfig(file, settings, ctx, 0); err != nil {
			return nil, err
		}
	}
	if settings.truthy("extensions.worktreeconfig") {
		if err := loadConfig(filepath.Join(r.GitDir, "config.worktree"), settings, ctx, 0); err != nil {
			return nil, err
		}
	}
	return settings, nil
}

func excludesFile(configured string) string {
	if configured == "" {
		if xdg, ok := xdgConfigHome(); ok {
			return filepath.Join(xdg, "git", "ignore")
		}
		return ""
	}
	return expandHome(configured)
}

func expandHome(path string) string {
	rest, ok := strings.CutPrefix(path, "~/")
	if !ok {
		return filepath.FromSlash(path)
	}
	home, ok := homeDir()
	if !ok {
		return ""
	}
	return filepath.Join(home, filepath.FromSlash(rest))
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

func truthyEnv(name string) bool {
	return config{"env": os.Getenv(name)}.truthy("env")
}

func readOptional(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return nil, nil
	}
	return data, err
}

func homeDir() (string, bool) {
	home, err := os.UserHomeDir()
	return home, err == nil && home != ""
}
