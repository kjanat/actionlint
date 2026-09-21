package actionlint

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type directoryKind uint8

const (
	directoryUnspecified directoryKind = iota
	directoryKnown
	directoryUnknown
)

type shellcheckDirectory struct {
	kind directoryKind
	path string
}

type shellcheckPaths struct {
	workspace string
	analysis  string
}

func workingDirectoryValue(value *String) shellcheckDirectory {
	if value == nil {
		return shellcheckDirectory{}
	}
	if !value.ContainsExpression() {
		return shellcheckDirectory{directoryKnown, value.Value}
	}
	if literal, known := workflowExpressionLiteral(value); known {
		if text, ok := workflowScalarString(literal); ok {
			return shellcheckDirectory{directoryKnown, text}
		}
	}
	return shellcheckDirectory{kind: directoryUnknown}
}

func defaultsWorkingDirectory(defaults *Defaults) shellcheckDirectory {
	if defaults == nil || defaults.Run == nil {
		return shellcheckDirectory{}
	}
	if defaults.Run.Expression == nil {
		return workingDirectoryValue(defaults.Run.WorkingDirectory)
	}
	value, known := workflowExpressionLiteral(defaults.Run.Expression)
	if !known || len(workflowExpressionLiteralErrors(workflowDefaultsRun, value, "defaults.run")) != 0 {
		return shellcheckDirectory{kind: directoryUnknown}
	}
	if object, ok := value.(map[string]any); ok {
		for key, field := range object {
			if strings.EqualFold(key, "working-directory") {
				if text, ok := workflowScalarString(field); ok {
					return shellcheckDirectory{directoryKnown, text}
				}
				return shellcheckDirectory{kind: directoryUnknown}
			}
		}
	}
	return shellcheckDirectory{}
}

func (rule *RuleShellcheck) stepDirectory(run *ExecRun) shellcheckDirectory {
	directory := shellcheckDirectory{directoryKnown, ""}
	for _, candidate := range []shellcheckDirectory{workingDirectoryValue(run.WorkingDirectory), rule.jobDir, rule.workflowDir} {
		if candidate.kind != directoryUnspecified {
			directory = candidate
			break
		}
	}
	unknown := shellcheckDirectory{directoryUnknown, rule.paths.analysis}
	if directory.kind == directoryUnknown {
		return unknown
	}
	// Runner-absolute paths refer to the remote machine; they are not local source roots.
	if filepath.IsAbs(directory.path) || strings.HasPrefix(directory.path, "/") || strings.Contains(directory.path, ":") {
		return unknown
	}
	path := filepath.Join(rule.paths.workspace, filepath.FromSlash(directory.path))
	relative, err := filepath.Rel(rule.paths.workspace, path)
	if err != nil || !filepath.IsLocal(relative) {
		return unknown
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return unknown
	}
	return shellcheckDirectory{directoryKnown, path}
}

func (rule *RuleShellcheck) prepareConfigPath() error {
	if rule.paths.analysis == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		rule.paths.analysis = cwd
	}
	if rule.paths.workspace == "" {
		rule.paths.workspace = rule.paths.analysis
	}
	configdir := rule.paths.workspace
	var selection ShellcheckConfigSelection
	if rule.config != nil && rule.config.Config != nil {
		selection = rule.config.Config
	} else if config := rule.Config(); config != nil {
		if config.filename != "" {
			configdir = filepath.Dir(config.filename)
		}
		if path := config.Tools.Shellcheck.Config.path(); path != "" {
			path, err := resolveShellcheckConfigPath(path, configdir, rule.paths.workspace)
			if err != nil {
				return err
			}
			selection = ShellcheckRCFile(path)
		}
	}
	rule.rcArgs = []string{"--norc"}
	var path string
	switch selected := selection.(type) {
	case nil:
		return nil
	case ShellcheckRCMode:
		if selected == ShellcheckRCDisabled {
			return nil
		}
		var err error
		path, err = discoverShellcheckRC(rule.paths.analysis)
		if err != nil {
			return err
		}
		if path == "" {
			return nil
		}
	case ShellcheckRCFile:
		var err error
		path, err = shellcheckRCFile(string(selected))
		if err != nil {
			return fmt.Errorf("tools.shellcheck.config: %w", err)
		}
	default:
		return errors.New("unsupported ShellCheck configuration selection")
	}
	rule.rcArgs = []string{"--rcfile", path}
	if rule.onInput != nil {
		rule.onInput(path)
	}
	return nil
}

func resolveShellcheckConfigPath(path, configdir, gitdir string) (string, error) {
	base := configdir
	for _, token := range []struct{ name, root string }{{"{configdir}", configdir}, {"{gitdir}", gitdir}} {
		if path == token.name || strings.HasPrefix(path, token.name+"/") || strings.HasPrefix(path, token.name+`\`) {
			base, path = token.root, strings.TrimLeft(strings.TrimPrefix(path, token.name), `/\`)
			break
		}
	}
	if strings.Contains(path, "{configdir}") || strings.Contains(path, "{gitdir}") || strings.HasPrefix(path, "{") {
		return "", errors.New("tools.shellcheck.config: use {configdir} or {gitdir} at the start of the path")
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	return filepath.Join(base, filepath.FromSlash(path)), nil
}

func shellcheckRCInDirectory(directory string) (string, error) {
	for _, name := range []string{".shellcheckrc", "shellcheckrc"} {
		path := filepath.Join(directory, name)
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("%q is not a regular ShellCheck configuration file", path)
		}
		return path, nil
	}
	return "", nil
}

func shellcheckRCFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		found, err := shellcheckRCInDirectory(path)
		if err != nil {
			return "", err
		}
		if found == "" {
			return "", fmt.Errorf("directory %q contains neither .shellcheckrc nor shellcheckrc", path)
		}
		path = found
	} else if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%q is not a regular ShellCheck configuration file", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return absPath(path), nil
}

func discoverShellcheckRC(start string) (string, error) {
	for directory := start; ; directory = filepath.Dir(directory) {
		path, err := shellcheckRCInDirectory(directory)
		if err != nil {
			return "", err
		}
		if path != "" {
			return shellcheckRCFile(path)
		}
		if filepath.Dir(directory) == directory {
			break
		}
	}
	var candidates []string
	if runtime.GOOS == "windows" {
		if config, err := os.UserConfigDir(); err == nil {
			candidates = append(candidates, filepath.Join(config, "shellcheckrc"))
		}
	} else {
		config := os.Getenv("XDG_CONFIG_HOME")
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, ".shellcheckrc"))
			if config == "" {
				config = filepath.Join(home, ".config")
			}
		}
		if config != "" {
			candidates = append(candidates, filepath.Join(config, "shellcheckrc"))
		}
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return "", err
		}
		return shellcheckRCFile(candidate)
	}
	return "", nil
}
