package gitrepo

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

const maxIncludeDepth = 10

type includeContext struct {
	gitDir     pathPair
	workTree   pathPair
	branch     string
	remoteURLs []string
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

func newIncludeContext(gitDir, workTree string) (*includeContext, error) {
	ctx := &includeContext{}
	if gitDir == "" {
		return ctx, nil
	}
	var err error
	if ctx.gitDir, err = newPathPair(gitDir); err != nil {
		return nil, err
	}
	if ctx.workTree, err = newPathPair(workTree); err != nil {
		return nil, err
	}
	head, err := readOptional(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return nil, err
	}
	if ref, ok := strings.CutPrefix(strings.TrimRight(string(head), "\r\n"), "ref: refs/heads/"); ok {
		ctx.branch = ref
	}
	return ctx, nil
}

type configLoader struct {
	settings    config
	ctx         *includeContext
	collectURLs bool
	remoteURLs  []string
}

func loadConfigFiles(files []string, ctx *includeContext) (config, error) {
	collector := &configLoader{settings: config{}, ctx: ctx, collectURLs: true}
	for _, file := range files {
		if err := collector.load(file, 0, false); err != nil {
			return nil, err
		}
	}
	ctx.remoteURLs = collector.remoteURLs
	loader := &configLoader{settings: config{}, ctx: ctx}
	for _, file := range files {
		if err := loader.load(file, 0, false); err != nil {
			return nil, err
		}
	}
	return loader.settings, nil
}

func (l *configLoader) load(path string, depth int, forbidRemoteURL bool) error {
	if depth > maxIncludeDepth {
		return fmt.Errorf("%s exceeds the maximum include depth", path)
	}
	data, err := readOptional(path)
	if err != nil || data == nil {
		return err
	}
	return parseConfig(data, func(section, name, value string) error {
		l.settings[section+"."+name] = value
		if strings.HasPrefix(section, "remote.") && name == "url" {
			if forbidRemoteURL {
				return errors.New("remote URLs cannot be configured in file directly or indirectly included by includeIf.hasconfig:remote.*.url")
			}
			l.remoteURLs = append(l.remoteURLs, value)
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
		matched, viaRemoteURL, err := l.matches(condition, path)
		if err != nil || !matched {
			return err
		}
		target, err := resolveInclude(value, filepath.Dir(path))
		if err != nil {
			return err
		}
		return l.load(target, depth+1, forbidRemoteURL || viaRemoteURL)
	})
}

func resolveInclude(include, dir string) (string, error) {
	expanded, ok := expandHome(include)
	if !ok {
		return "", fmt.Errorf("could not expand include path %q", include)
	}
	expanded = filepath.FromSlash(expanded)
	if isAbsolutePath(expanded) {
		return expanded, nil
	}
	return filepath.Join(dir, expanded), nil
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
		if ctx.branch == "" {
			return false, false, nil
		}
		pattern := strings.TrimPrefix(condition, "onbranch:")
		if strings.HasSuffix(pattern, "/") {
			pattern += "**"
		}
		return wildmatch(pattern, ctx.branch, false), false, nil
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
	if expanded, ok := expandHome(pattern); ok {
		pattern = expanded
	}
	prefix := ""
	switch {
	case strings.HasPrefix(pattern, "./") || strings.HasPrefix(pattern, `.\`):
		dir, err := filepath.EvalSymlinks(filepath.Dir(configFile))
		if err != nil {
			return false, err
		}
		prefix = filepath.ToSlash(dir) + "/"
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
	return strings.HasPrefix(path, "/") || strings.HasPrefix(path, `\`) || filepath.IsAbs(filepath.FromSlash(path)) || (len(path) >= 2 && path[1] == ':' && isAlpha(path[0]))
}

func expandHome(path string) (string, bool) {
	if !strings.HasPrefix(path, "~") {
		return path, true
	}
	name, rest, _ := strings.Cut(path[1:], "/")
	if name == "" {
		home, ok := homeDir()
		if !ok {
			return "", false
		}
		return home + "/" + rest, true
	}
	account, err := user.Lookup(name)
	if err != nil || account.HomeDir == "" {
		return "", false
	}
	return account.HomeDir + "/" + rest, true
}

func homeDir() (string, bool) {
	home, err := os.UserHomeDir()
	return home, err == nil && home != ""
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
