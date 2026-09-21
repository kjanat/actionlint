package actionlint

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// configPathContext contains the local directories available to path interpolation.
// actionPath identifies the composite action being analyzed.
type configPathContext struct {
	configDir  string
	gitDir     string
	workspace  string
	actionPath string
}

var errConfigActionPathUnavailable = errors.New("path interpolation \"github.action_path\" requires an analyzed composite action")

func (rule *RuleShellcheck) pathContext() configPathContext {
	configDir := rule.paths.workspace
	if config := rule.Config(); config != nil && config.filename != "" {
		configDir = filepath.Dir(config.filename)
	}
	workspace := os.Getenv("GITHUB_WORKSPACE")
	if workspace == "" {
		workspace = rule.paths.workspace
	}
	actionPath := rule.actionPath
	if runnerPath := os.Getenv("GITHUB_ACTION_PATH"); actionPath != "" && runnerPath != "" {
		local, localErr := os.Stat(actionPath)
		runner, runnerErr := os.Stat(runnerPath)
		if localErr == nil && runnerErr == nil && os.SameFile(local, runner) {
			actionPath = runnerPath
		}
	}
	return configPathContext{
		configDir: configDir, gitDir: rule.paths.workspace,
		workspace: workspace, actionPath: actionPath,
	}
}

func (context configPathContext) expand(value string) (string, error) {
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
			path = context.configDir
		case "gitdir":
			path = context.gitDir
		case "github.workspace":
			path = context.workspace
		case "github.action_path":
			path = context.actionPath
			if path == "" {
				return "", errConfigActionPathUnavailable
			}
		default:
			return "", fmt.Errorf("unknown path interpolation %q; use configdir, gitdir, github.workspace or github.action_path", name)
		}
		if path == "" {
			return "", fmt.Errorf("path interpolation %q is unavailable in this analysis context", name)
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

func (context configPathContext) resolve(value string) (string, error) {
	return context.resolveFrom(value, context.configDir)
}

func (context configPathContext) resolveFrom(value, base string) (string, error) {
	path, err := context.expand(value)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	return filepath.Join(base, filepath.FromSlash(path)), nil
}
