package repoconfig

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// PathContext contains the local directories available to path interpolation.
// ActionPath identifies the composite action being analyzed.
type PathContext struct {
	ConfigDir  string
	GitDir     string
	Workspace  string
	ActionPath string
}

var ErrActionPathUnavailable = errors.New("path interpolation \"github.action_path\" requires an analyzed composite action")

func (context PathContext) Expand(value string) (string, error) {
	var out strings.Builder
	for {
		before, expression, found := strings.Cut(value, "${{")
		if strings.Contains(before, "{configdir}") || strings.Contains(before, "{gitdir}") {
			return "", errors.New("use ${{ configdir }} or ${{ gitdir }} for path interpolation")
		}
		out.WriteString(before)
		if !found {
			break
		}
		name, after, closed := strings.Cut(expression, "}}")
		if !closed {
			return "", fmt.Errorf("unterminated path interpolation in %q; expected }}", value)
		}
		name = strings.ToLower(strings.TrimSpace(name))
		var path string
		switch name {
		case "configdir":
			path = context.ConfigDir
		case "gitdir":
			path = context.GitDir
		case "github.workspace":
			path = context.Workspace
		case "github.action_path":
			path = context.ActionPath
			if path == "" {
				return "", ErrActionPathUnavailable
			}
		default:
			return "", fmt.Errorf("unknown path interpolation %q; use configdir, gitdir, github.workspace or github.action_path", name)
		}
		if path == "" {
			return "", fmt.Errorf("path interpolation %q is unavailable in this analysis context", name)
		}
		path, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("path interpolation %q: %w", name, err)
		}
		out.WriteString(path)
		value = after
	}
	result := out.String()
	if strings.ContainsAny(result, "\x00\r\n") {
		return "", errors.New("path interpolation must produce a single-line path without NUL bytes")
	}
	return result, nil
}

func (context PathContext) Resolve(value string) (string, error) {
	return context.ResolveFrom(value, context.ConfigDir)
}

func (context PathContext) ResolveFrom(value, base string) (string, error) {
	path, err := context.Expand(value)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	return filepath.Join(base, filepath.FromSlash(path)), nil
}
