package gitrepo

import (
	"fmt"
	"path/filepath"
	"strings"
)

const maxIncludeDepth = 10

type includeContext struct {
	gitDir string
	branch string
}

func newIncludeContext(gitDir string) (*includeContext, error) {
	ctx := &includeContext{}
	if gitDir == "" {
		return ctx, nil
	}
	physical, err := filepath.EvalSymlinks(gitDir)
	if err != nil {
		return nil, err
	}
	ctx.gitDir = filepath.ToSlash(physical)
	head, err := readOptional(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return nil, err
	}
	if ref, ok := strings.CutPrefix(strings.TrimRight(string(head), "\r\n"), "ref: refs/heads/"); ok {
		ctx.branch = ref
	}
	return ctx, nil
}

func loadConfig(path string, into config, ctx *includeContext, depth int) error {
	if depth > maxIncludeDepth {
		return fmt.Errorf("%s exceeds the maximum include depth", path)
	}
	data, err := readOptional(path)
	if err != nil || data == nil {
		return err
	}
	dir := filepath.Dir(path)
	return parseConfig(data, into, func(condition, include string) error {
		matched, err := ctx.matches(condition, dir, into)
		if err != nil || !matched {
			return err
		}
		target := resolveInclude(include, dir)
		if target == "" {
			return nil
		}
		return loadConfig(target, into, ctx, depth+1)
	})
}

func resolveInclude(include, dir string) string {
	if strings.HasPrefix(include, "~/") {
		return expandHome(include)
	}
	include = filepath.FromSlash(include)
	if filepath.IsAbs(include) {
		return include
	}
	return filepath.Join(dir, include)
}

func (c *includeContext) matches(condition, dir string, settings config) (bool, error) {
	if condition == "" {
		return true, nil
	}
	keyword, pattern, ok := strings.Cut(condition, ":")
	if !ok {
		return false, fmt.Errorf("includeIf condition %q has no pattern", condition)
	}
	switch keyword {
	case "gitdir", "gitdir/i":
		if c.gitDir == "" {
			return false, nil
		}
		pattern, ok := gitdirPattern(pattern, dir)
		return ok && wildmatch(pattern, c.gitDir, keyword == "gitdir/i"), nil
	case "onbranch":
		if c.branch == "" {
			return false, nil
		}
		if strings.HasSuffix(pattern, "/") {
			pattern += "**"
		}
		return wildmatch(pattern, c.branch, false), nil
	case "hasconfig":
		key, pattern, ok := strings.Cut(pattern, ":")
		if !ok || key != "remote.*.url" {
			return false, fmt.Errorf("unsupported includeIf condition %q", condition)
		}
		for name, value := range settings {
			if strings.HasPrefix(name, "remote.") && strings.HasSuffix(name, ".url") && wildmatch(pattern, value, false) {
				return true, nil
			}
		}
		return false, nil
	}
	return false, fmt.Errorf("unsupported includeIf condition %q", condition)
}

func gitdirPattern(pattern, dir string) (string, bool) {
	switch {
	case strings.HasPrefix(pattern, "~/"):
		home, ok := homeDir()
		if !ok {
			return "", false
		}
		pattern = filepath.ToSlash(home) + pattern[1:]
	case strings.HasPrefix(pattern, "./"):
		pattern = filepath.ToSlash(dir) + pattern[1:]
	case !strings.HasPrefix(pattern, "/") && !strings.HasPrefix(pattern, "**/") && !filepath.IsAbs(filepath.FromSlash(pattern)):
		pattern = "**/" + pattern
	}
	if strings.HasSuffix(pattern, "/") {
		pattern += "**"
	}
	return pattern, true
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
