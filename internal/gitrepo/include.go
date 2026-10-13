package gitrepo

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

const maxIncludeDepth = 10

type includeContext struct {
	disk          disk
	gitDir        pathPair
	workTree      pathPair
	resolveBranch func() (string, error)
	branch        string
	resolved      bool
	remoteURLs    []string
}

func (c *includeContext) currentBranch() (string, error) {
	if !c.resolved && c.resolveBranch != nil {
		branch, err := c.resolveBranch()
		if err != nil {
			return "", err
		}
		c.branch = branch
	}
	c.resolved = true
	return c.branch, nil
}

type pathPair struct {
	real, absolute string
}

func newPathPair(path string) (pathPair, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return pathPair{}, err
	}
	absolute = longPath(absolute)
	pair := pathPair{real: filepath.ToSlash(absolute), absolute: filepath.ToSlash(absolute)}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		pair.real = filepath.ToSlash(longPath(resolved))
	}
	return pair, nil
}

func newIncludeContext(gitDir, unresolvedGitDir, workTree string, branch func() (string, error), d disk) (*includeContext, error) {
	ctx := &includeContext{disk: d}
	if gitDir == "" {
		return ctx, nil
	}
	var err error
	if ctx.gitDir, err = newPathPair(gitDir); err != nil {
		return nil, err
	}
	if unresolvedGitDir != "" {
		absolute, err := filepath.Abs(unresolvedGitDir)
		if err != nil {
			return nil, err
		}
		ctx.gitDir.absolute = filepath.ToSlash(longPath(absolute))
	}
	if workTree != "" {
		if ctx.workTree, err = newPathPair(workTree); err != nil {
			return nil, err
		}
	}
	ctx.resolveBranch = branch
	return ctx, nil
}

type configLoader struct {
	settings    config
	ctx         *includeContext
	collectURLs bool
	remoteURLs  []string
}

func loadConfigFiles(files []string, params []configParam, ctx *includeContext) (config, error) {
	collector := &configLoader{settings: config{}, ctx: ctx, collectURLs: true}
	if err := collector.loadAll(files, params); err != nil {
		return nil, err
	}
	ctx.remoteURLs = collector.remoteURLs
	loader := &configLoader{settings: config{}, ctx: ctx}
	if err := loader.loadAll(files, params); err != nil {
		return nil, err
	}
	return loader.settings, nil
}

func (l *configLoader) loadAll(files []string, params []configParam) error {
	for _, file := range files {
		if err := l.load(file, 0, false); err != nil {
			return err
		}
	}
	for _, param := range params {
		if err := l.visit("", 0, false, param.section, param.name, param.value); err != nil {
			return fmt.Errorf("unable to parse command-line config: %w", err)
		}
	}
	return nil
}

func (l *configLoader) load(path string, depth int, forbidRemoteURL bool) error {
	if depth > maxIncludeDepth {
		return fmt.Errorf("%s exceeds the maximum include depth", path)
	}
	data, err := l.ctx.disk.readOptional(path)
	if err != nil || data == nil {
		return err
	}
	return parseConfig(path, data, func(section, name string, value configValue) error {
		return l.visit(path, depth, forbidRemoteURL, section, name, value)
	})
}

func (l *configLoader) visit(file string, depth int, forbidRemoteURL bool, section, name string, value configValue) error {
	l.settings.add(section, name, value)
	if strings.HasPrefix(section, "remote.") && name == "url" {
		if forbidRemoteURL {
			return errors.New("remote URLs cannot be configured in file directly or indirectly included by includeIf.hasconfig:remote.*.url")
		}
		if !value.bare {
			l.remoteURLs = append(l.remoteURLs, value.text)
		}
	}
	if name != "path" {
		return nil
	}
	condition, conditional := strings.CutPrefix(section, "includeif.")
	switch {
	case section == "include":
		condition = ""
	case !conditional:
		return nil
	}
	matched, viaRemoteURL, err := l.matches(condition, file)
	if err != nil || !matched {
		return err
	}
	if value.bare {
		return errors.New("missing value for 'include.path'")
	}
	target, err := resolveInclude(value.text, file)
	if err != nil {
		return err
	}
	return l.load(target, depth+1, forbidRemoteURL || viaRemoteURL)
}

func resolveInclude(include, file string) (string, error) {
	expanded, ok := expandHome(include, false)
	if !ok {
		return "", fmt.Errorf("could not expand include path %q", include)
	}
	expanded = filepath.FromSlash(expanded)
	if isAbsolutePath(expanded) {
		return expanded, nil
	}
	if file == "" {
		return "", errors.New("relative config includes must come from files")
	}
	return filepath.Join(filepath.Dir(file), expanded), nil
}

func (l *configLoader) matches(condition, configFile string) (bool, bool, error) {
	if condition == "" {
		return true, false, nil
	}
	ctx := l.ctx
	switch {
	case strings.HasPrefix(condition, "gitdir:"):
		ok, err := matchIncludePath(ctx.gitDir, strings.TrimPrefix(condition, "gitdir:"), configFile, false)
		return ok, false, err
	case strings.HasPrefix(condition, "gitdir/i:"):
		ok, err := matchIncludePath(ctx.gitDir, strings.TrimPrefix(condition, "gitdir/i:"), configFile, true)
		return ok, false, err
	case strings.HasPrefix(condition, "worktree:"):
		ok, err := matchIncludePath(ctx.workTree, strings.TrimPrefix(condition, "worktree:"), configFile, false)
		return ok, false, err
	case strings.HasPrefix(condition, "worktree/i:"):
		ok, err := matchIncludePath(ctx.workTree, strings.TrimPrefix(condition, "worktree/i:"), configFile, true)
		return ok, false, err
	case strings.HasPrefix(condition, "onbranch:"):
		branch, err := ctx.currentBranch()
		if err != nil {
			return false, false, err
		}
		if branch == "" {
			return false, false, nil
		}
		pattern := strings.TrimPrefix(condition, "onbranch:")
		if strings.HasSuffix(pattern, "/") {
			pattern += "**"
		}
		return wildmatch(pattern, branch, false), false, nil
	case strings.HasPrefix(condition, "hasconfig:remote.*.url:"):
		if l.collectURLs {
			return true, true, nil
		}
		pattern := strings.TrimPrefix(condition, "hasconfig:remote.*.url:")
		for _, url := range ctx.remoteURLs {
			if wildmatch(pattern, url, false) {
				return true, true, nil
			}
		}
		return false, true, nil
	}
	return false, false, nil
}

func matchIncludePath(subject pathPair, pattern, configFile string, icase bool) (bool, error) {
	if subject.absolute == "" {
		return false, nil
	}
	if expanded, ok := expandHome(pattern, true); ok {
		pattern = expanded
	}
	prefix := ""
	switch {
	case (strings.HasPrefix(pattern, "./") || strings.HasPrefix(pattern, `.\`)) && configFile == "":
		return true, nil
	case strings.HasPrefix(pattern, "./") || strings.HasPrefix(pattern, `.\`):
		resolved, err := filepath.EvalSymlinks(configFile)
		if err != nil {
			return false, err
		}
		prefix = filepath.ToSlash(filepath.Dir(resolved)) + "/"
		pattern = prefix + pattern[2:]
	case !isAbsolutePath(pattern):
		pattern = "**/" + pattern
	}
	if strings.HasSuffix(pattern, "/") || strings.HasSuffix(pattern, `\`) {
		pattern += "**"
	}
	for _, text := range []string{subject.real, subject.absolute} {
		if len(text) < len(prefix) {
			continue
		}
		head, tail := text[:len(prefix)], text[len(prefix):]
		if (icase && !strings.EqualFold(head, prefix)) || (!icase && head != prefix) {
			continue
		}
		if wildmatch(pattern[len(prefix):], tail, icase) {
			return true, nil
		}
	}
	return false, nil
}

func isAbsolutePath(path string) bool {
	return absolutePathOn(runtime.GOOS, path)
}

func absolutePathOn(goos, path string) bool {
	if strings.HasPrefix(path, "/") {
		return true
	}
	if goos != "windows" {
		return false
	}
	return strings.HasPrefix(path, `\`) || (len(path) >= 2 && path[1] == ':' && isAlpha(path[0]))
}

func expandHome(path string, realHome bool) (string, bool) {
	if rest, ok := strings.CutPrefix(path, "%(prefix)/"); ok {
		if isAbsolutePath(rest) {
			return rest, true
		}
		return gitPrefix() + "/" + rest, true
	}
	if !strings.HasPrefix(path, "~") {
		return path, true
	}
	name, rest := path[1:], ""
	if slash := strings.IndexByte(path, '/'); slash >= 0 {
		name, rest = path[1:slash], path[slash:]
	}
	if name == "" {
		home, ok := homeDir()
		if !ok {
			return "", false
		}
		if realHome {
			if resolved, err := filepath.EvalSymlinks(home); err == nil {
				home = resolved
			}
		}
		return filepath.ToSlash(home) + rest, true
	}
	account, err := user.Lookup(name)
	if err != nil {
		return "", false
	}
	return account.HomeDir + rest, true
}

func homeDir() (string, bool) {
	return lookupHome(runtime.GOOS == "windows", os.LookupEnv)
}

func lookupHome(windows bool, getenv func(string) (string, bool)) (string, bool) {
	if home, ok := getenv("HOME"); ok || !windows {
		return home, ok
	}
	drive, hasDrive := getenv("HOMEDRIVE")
	path, hasPath := getenv("HOMEPATH")
	if info, err := os.Stat(drive + path); hasDrive && hasPath && err == nil && info.IsDir() {
		return drive + path, true
	}
	return getenv("USERPROFILE")
}

func wildmatch(pattern, text string, fold bool) bool {
	pattern = strings.TrimPrefix(pattern, "/")
	text = strings.TrimPrefix(text, "/")
	matcher := parseMatcher([]byte("/"+pattern), fold)
	if matcher == nil {
		return false
	}
	name := text
	if index := strings.LastIndexByte(text, '/'); index >= 0 {
		name = text[index+1:]
	}
	return matcher.matches([]byte(text), []byte(name), fold)
}
