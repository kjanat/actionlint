package actionlint

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

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
	var selection ShellcheckConfigSelection
	base := rule.pathContext().configDir
	if rule.config != nil && rule.config.Config != nil {
		selection = rule.config.Config
		base = "" // Application paths retain their process-working-directory origin.
	} else if config := rule.Config(); config != nil {
		if path := config.Tools.Shellcheck.Config.path(); path != "" {
			selection = ShellcheckRCFile(path)
		}
	}
	rcArgs := []string{"--norc"}
	var path string
	switch selected := selection.(type) {
	case nil:
		rule.rcArgs = rcArgs
		return nil
	case ShellcheckRCMode:
		if selected == ShellcheckRCDisabled {
			rule.rcArgs = rcArgs
			return nil
		}
		var err error
		path, err = discoverShellcheckRC(rule.paths.analysis)
		if err != nil {
			return err
		}
		if path == "" {
			rule.rcArgs = rcArgs
			return nil
		}
	case ShellcheckRCFile:
		var err error
		path, err = rule.pathContext().resolveFrom(string(selected), base)
		if err == nil {
			path, err = shellcheckRCFile(path)
		}
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
